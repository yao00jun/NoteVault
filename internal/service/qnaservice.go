package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// RAG 知识库问答
// ---------------------------------------------------------------------------

// QnACitation 表示回答中引用的一篇知识库文档
type QnACitation struct {
	Index   int    `json:"index"`   // 引用编号（从 1 开始，与回答中的 [n] 对应）
	Path    string `json:"path"`    // 文档相对路径
	Title   string `json:"title"`   // 文档标题
	Snippet string `json:"snippet"` // 命中片段摘要
}

// QnAResponse 是一次问答的完整结果
type QnAResponse struct {
	Answer    string        `json:"answer"`    // Markdown 格式的回答
	Citations []QnACitation `json:"citations"` // 引用的文档列表
}

// QnA 服务的检索与上下文预算参数。
// 原先是包级 var（注释称"方便测试中临时调整"），但全仓库无任何用例改值，
// 只留下进程级全局可变状态的风险。降级为 const，编译期锁定。
const (
	qnaTopK          = 5     // 检索返回的文档数上限
	qnaMaxDocRunes   = 2500  // 单篇文档注入上下文的长度上限（字符）
	qnaMaxTotalRunes = 12000 // 全部上下文的总长度上限（字符）
)

// QnAService 提供基于 RAG（检索增强生成）的知识库问答能力。
// 设计：
//  1. 检索 —— 复用 searchindex 的内存反向索引（与全文搜索同一份缓存，增量更新），
//     按「问题 token 与文档 token 的重合度」评分并取 Top-K；
//  2. 生成 —— 将 Top-K 文档内容作为上下文注入 system prompt，
//     调用 OpenAI 兼容 Chat Completions API 生成带 [n] 引用标注的回答。
//
// API Key 仅由前端在调用时传入，Go 端不落盘存储，保障隐私。
type QnAService struct {
	client *http.Client
}

// NewQnAService 创建问答服务实例
func NewQnAService() *QnAService {
	return &QnAService{
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// qnaScoredDoc 检索评分中间结构
type qnaScoredDoc struct {
	doc   *cachedDoc
	score int
}

// retrieve 基于反向索引检索与问题最相关的 Top-K 文档。
// 评分规则：
//   - 正文每命中一个（去重后的）问题 token +1 分
//   - 标题每命中一个问题 token 额外 +2 分（标题命中的文档更可能是主题文档）
func (s *QnAService) retrieve(workspacePath, question string) []*cachedDoc {
	idx := getSearchIndex(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return nil
	}

	queryTokens := tokenize(question)
	if len(queryTokens) == 0 {
		return nil
	}
	tokenSet := make(map[string]bool, len(queryTokens))
	for _, qt := range queryTokens {
		tokenSet[qt] = true
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	scored := make([]qnaScoredDoc, 0, len(idx.docs))
	for _, doc := range idx.docs {
		score := 0
		titleTokens := make(map[string]bool)
		for _, tt := range tokenize(doc.title) {
			titleTokens[tt] = true
		}
		for qt := range tokenSet {
			if doc.tokenSet[qt] {
				score++
				if titleTokens[qt] {
					score += 2
				}
			}
		}
		if score > 0 {
			scored = append(scored, qnaScoredDoc{doc: doc, score: score})
		}
	}

	// 按得分降序；得分相同按路径排序保证结果稳定
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].doc.relPath < scored[j].doc.relPath
	})

	if len(scored) > qnaTopK {
		scored = scored[:qnaTopK]
	}

	docs := make([]*cachedDoc, 0, len(scored))
	for _, sd := range scored {
		docs = append(docs, sd.doc)
	}
	return docs
}

// Answer 对知识库提问并返回带引用的回答
//   - apiKey: 用户 API Key（本地存储，不落盘）
//   - baseURL: OpenAI 兼容接口地址
//   - model: 模型名称
//   - workspacePath: 工作区根目录
//   - question: 用户问题
func (s *QnAService) Answer(apiKey, baseURL, model, workspacePath, question string) (*QnAResponse, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("请先在「设置 → AI 总结」中填写 API Key")
	}
	if strings.TrimSpace(workspacePath) == "" {
		return nil, fmt.Errorf("未选择工作区")
	}

	idx := getSearchIndex(workspacePath)
	docs := s.retrieve(workspacePath, question)

	// 正文按需加载：问答只吃 Top-K 篇，先把这几篇的正文读出来，
	// 之后统一用 loaded，避免全库正文常驻内存（正文受索引预算约束）。
	type qnaDoc struct {
		relPath string
		title   string
		content string
	}
	loaded := make([]qnaDoc, 0, len(docs))
	for _, doc := range docs {
		content, _, err := idx.contentOf(doc)
		if err != nil {
			// 文档已被并发 refresh 重建/删除，或读取失败 → 跳过
			continue
		}
		loaded = append(loaded, qnaDoc{relPath: doc.relPath, title: doc.title, content: content})
	}
	idx.enforceContentBudget()

	citations := make([]QnACitation, 0, len(loaded))
	for i, d := range loaded {
		citations = append(citations, QnACitation{
			Index:   i + 1,
			Path:    d.relPath,
			Title:   d.title,
			Snippet: extractSnippet(d.content, strings.Fields(question)[0], 60),
		})
	}

	if len(loaded) == 0 {
		// 知识库中未检索到相关内容，直接返回提示而不调用 LLM
		return &QnAResponse{
			Answer:    "未在当前知识库中检索到与该问题相关的内容。请尝试换个问法，或先补充相关笔记。",
			Citations: citations,
		}, nil
	}

	// 组装上下文（截断单篇与总长度）
	var ctx strings.Builder
	total := 0
	for i, doc := range loaded {
		content := []rune(doc.content)
		if len(content) > qnaMaxDocRunes {
			content = content[:qnaMaxDocRunes]
		}
		if total+len(content) > qnaMaxTotalRunes {
			content = content[:max(0, qnaMaxTotalRunes-total)]
		}
		fmt.Fprintf(&ctx, "[文档 %d] %s\n%s\n\n", i+1, doc.relPath, string(content))
		total += len(content)
		if total >= qnaMaxTotalRunes {
			break
		}
	}

	systemPrompt := "你是个人知识库问答助手。请仅依据下面提供的知识库文档内容回答用户问题，" +
		"并遵循以下规则：\n" +
		"1. 回答使用与用户问题相同的语言；\n" +
		"2. 在引用某篇文档的位置标注 [n]（n 为文档编号），例如 [1] [2]；\n" +
		"3. 如果文档中没有足够信息回答问题，请如实说明知识库中缺少相关内容；\n" +
		"4. 使用 Markdown 输出，简洁准确，不要编造文档中不存在的内容。\n\n" +
		"知识库文档：\n" + ctx.String()

	reqBody := chatRequest{
		Model:  model,
		Stream: false,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: question},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败：%w", err)
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 AI 服务失败（请检查网络与 BaseURL）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI 服务返回错误（%d）：%s", resp.StatusCode, string(respData))
	}

	var cr chatResponse
	if err := json.Unmarshal(respData, &cr); err != nil {
		return nil, fmt.Errorf("解析 AI 响应失败：%w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, fmt.Errorf("AI 服务错误：%s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("AI 服务未返回内容")
	}

	return &QnAResponse{
		Answer:    strings.TrimSpace(cr.Choices[0].Message.Content),
		Citations: citations,
	}, nil
}
