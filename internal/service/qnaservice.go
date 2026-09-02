package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// RAG 知识库问答
// ---------------------------------------------------------------------------

// QnACitation 表示回答中引用的一处知识库内容
type QnACitation struct {
	Index   int    `json:"index"`   // 引用编号（从 1 开始，与回答中的 [n] 对应）
	Path    string `json:"path"`    // 文档相对路径
	Title   string `json:"title"`   // 文档标题
	Snippet string `json:"snippet"` // 命中片段摘要
	// 以下两项是 P0-2 分块检索的产物，让引用能定位到具体章节而不是整篇。
	// 前端暂未使用，属于向前兼容地扩展（不读不影响既有行为）。
	Heading    string `json:"heading"`    // 该块所属的最近上级标题
	ChunkIndex int    `json:"chunkIndex"` // 块在文档内的序号，从 0 开始
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
	// qnaTopKChunks 注入上下文的块数上限。
	// 社区实测的甜点是 5-8 块：再往上加，模型会被互相矛盾或重复的信息干扰，
	// 回答质量反而下降。
	qnaTopKChunks = 8
	// qnaCandidateDocs 先取多少篇候选文档再分块。
	// 分块 + 块级打分比文档级检索贵得多，因此先用便宜的文档级 BM25 收敛范围，
	// 而不是对全库分块。
	qnaCandidateDocs = 20
	// qnaChunkMaxRunes 单块目标长度（字符）。
	qnaChunkMaxRunes = 700
	// qnaChunkOverlapRunes 相邻块的重叠长度，约为目标长度的 10%。
	qnaChunkOverlapRunes = 70
	// qnaMaxTotalRunes 全部上下文的总长度预算（字符）。
	qnaMaxTotalRunes = 12000
	// qnaSnippetRunes 引用里展示的片段长度（字符）。
	qnaSnippetRunes = 120
	// qnaRerankPool 交重排序的候选块池上限（P1-3b）。
	// 先宽召回再重排是 rerank 的标准范式：取融合后的 Top-N 候选（远大于最终注入的
	// qnaTopKChunks=8），由 cross-encoder 在「query + 候选」联合视角下重排，挑最相关进上下文。
	qnaRerankPool = 30
)

// QnAService 提供基于 RAG（检索增强生成）的知识库问答能力。
// 设计：
//  1. 检索 —— 复用 searchindex 的内存反向索引（与全文搜索同一份缓存，增量更新），
//     按「问题 token 与文档 token 的重合度」评分并取 Top-K；
//  2. 生成 —— 将 Top-K 文档内容作为上下文注入 system prompt，
//     调用 OpenAI 兼容 Chat Completions API 生成带 [n] 引用标注的回答。
//
// P1-3 增强：配置 embedding 端点后，检索在块级融合「向量 leg」（chromem 包装的
// 语义检索）与既有 BM25 leg（RRF 加权）。未配置 embedding 时，融合层直接退化
// 为纯 BM25，行为与 P0 结束态逐字节一致（红线 #1）。
//
// API Key 仅由前端在调用时传入，Go 端不落盘存储，保障隐私。
type QnAService struct {
	client *http.Client
	// indexes 索引注册表（E-4）。nil 时回落到包级默认注册表，零值实例可用。
	indexes *SearchIndexRegistry
	// fileService 构建向量索引时读取 Markdown 正文。nil 时向量 leg 不可用（降级）。
	fileService *FileService
	// embedder 抽象 embedding 客户端。nil 或 NoopEmbedder 表示未配置，走降级。
	embedder Embedder
	// reranker 抽象重排序客户端（P1-3b）。nil 或 NoopReranker 表示未配置，
	// 检索融合退化为纯 BM25 + 向量 RRF，行为与 P1-3 结束态一致（红线 #1）。
	reranker Reranker

	// vectorMu 保护 vectorCache。向量索引构建/加载较重，按工作区缓存避免每次重建。
	vectorMu    sync.Mutex
	vectorCache map[string]*VectorStore
}

// NewQnAService 创建问答服务实例（索引走包级默认注册表，未配置 embedding/rerank 降级）。
// 仅用于测试与零值场景；生产装配请用 NewQnAServiceWithRegistry。
func NewQnAService() *QnAService {
	return &QnAService{
		client:      &http.Client{Timeout: 120 * time.Second},
		embedder:    &NoopEmbedder{},
		reranker:    &NoopReranker{},
		vectorCache: make(map[string]*VectorStore),
	}
}

// NewQnAServiceWithRegistry 创建问答服务实例，并显式指定索引注册表与依赖。
//
// 问答与全文搜索共用同一份索引，因此必须与 SearchService 传入同一个注册表实例，
// 否则会各建一份、内存翻倍且增量更新互不可见。
// embedder 为 nil 时回落到 NoopEmbedder（向量 leg 不可用，检索退化为纯 BM25）。
// reranker 为 nil 时回落到 NoopReranker（重排不可用，检索退化为纯 RRF 融合）。
func NewQnAServiceWithRegistry(fileService *FileService, indexes *SearchIndexRegistry, embedder Embedder, reranker Reranker) *QnAService {
	if embedder == nil {
		embedder = &NoopEmbedder{}
	}
	if reranker == nil {
		reranker = &NoopReranker{}
	}
	return &QnAService{
		client:      &http.Client{Timeout: 120 * time.Second},
		indexes:     indexes,
		fileService: fileService,
		embedder:    embedder,
		reranker:    reranker,
		vectorCache: make(map[string]*VectorStore),
	}
}

// idxOf 返回工作区对应的索引。nil 注册表回落到包级默认注册表。
func (s *QnAService) idxOf(workspacePath string) *searchIndex {
	if s.indexes != nil {
		return s.indexes.Get(workspacePath)
	}
	return defaultIndexRegistry.Get(workspacePath)
}

// retrieveChunks 检索与问题最相关的 Top-K 文本块（P0-2）。
//
// 两阶段，先便宜后昂贵：
//  1. 文档级 BM25 取候选——纯内存、走倒排表、不读正文，
//     把范围从「全库」收敛到 qnaCandidateDocs 篇；
//  2. 只对候选文档分块并做块级打分——这步要读正文并逐块分词，贵得多。
//
// 不对全库分块：那等于每次提问都把整个知识库重新切一遍。
func (s *QnAService) retrieveChunks(workspacePath, question string) []scoredChunk {
	queryTokens := tokenize(question)
	if len(queryTokens) == 0 {
		return nil
	}

	idx := s.idxOf(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return nil
	}

	// 阶段 1：文档级候选（持读锁，纯内存打分）
	idx.mu.RLock()
	docScores := idx.scoreBM25(queryTokens, qnaCandidateDocs)
	idx.mu.RUnlock()

	// 阶段 2：分块 + 块级打分。
	// 正文读取在锁外——contentOf 可能触发磁盘 IO，持锁会阻塞并发 refresh。
	var chunks []chunk
	for _, sd := range docScores {
		content, _, err := idx.contentOf(sd.doc)
		if err != nil {
			// 文档已被并发 refresh 重建/删除，或读取失败 → 跳过
			continue
		}
		chunks = append(chunks, chunkDocument(
			sd.doc.relPath, sd.doc.title, content,
			qnaChunkMaxRunes, qnaChunkOverlapRunes,
		)...)
	}
	idx.enforceContentBudget()

	scored := scoreChunks(chunks, queryTokens, idx)
	if len(scored) > qnaTopKChunks {
		scored = scored[:qnaTopKChunks]
	}
	return scored
}

// Answer 对知识库提问并返回带引用的回答
//   - apiKey: 用户 API Key（本地存储，不落盘）
//   - baseURL: OpenAI 兼容接口地址（Chat/生成）
//   - model: 模型名称
//   - embBaseURL / embModel / embAPIKey: 语义检索用的 embedding 端点配置
//     （与生成端点独立，P1-6 #2）。三者为空时向量 leg 不启用，退化为纯 BM25。
//   - rerankCfg: 重排序端点配置（P1-3b，可选）。未配置时检索融合退化为纯 BM25 + 向量 RRF，
//     与 P1-3 结束态一致；重排服务不可用时静默回退，不阻断问答。
//   - workspacePath: 工作区根目录
//   - question: 用户问题
//
// 注：rerankCfg 用结构体而非 3~4 个裸 string 传入（对比 embedding 的三裸 string），
// 是 P1-3 之后的演进——配置字段更多（provider/baseURL/model/apiKey）时，结构体比
// 一长串位置参数更易读、不易错位。embedding 的裸 string 签名已在 P1-3 落地，不回溯重构。
func (s *QnAService) Answer(apiKey, baseURL, model, embBaseURL, embModel, embAPIKey string, rerankCfg RerankConfig, workspacePath, question string) (*QnAResponse, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	// 本机端点（Ollama / LM Studio）不需要 Key，requireCredential 会放行
	credential, err := requireCredential(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspacePath) == "" {
		return nil, fmt.Errorf("未选择工作区")
	}

	embCfg := EmbeddingConfig{BaseURL: embBaseURL, Model: embModel, APIKey: embAPIKey}
	scored := s.retrieveChunksHybrid(workspacePath, question, embCfg, rerankCfg)

	// 组装上下文：按块累计，受总预算约束。
	// 与旧实现的差别是「截断单位」——旧的是每篇砍到 2500 字（砍掉的是后半篇），
	// 现在每块本身就在 700 字量级，总预算只用来决定能塞进几个块。
	citations := make([]QnACitation, 0, len(scored))
	var ctx strings.Builder
	total := 0
	for _, sc := range scored {
		text := []rune(sc.chunk.text)
		if total+len(text) > qnaMaxTotalRunes {
			text = text[:max(0, qnaMaxTotalRunes-total)]
		}
		if len(text) == 0 {
			break
		}
		citations = append(citations, QnACitation{
			Index:      len(citations) + 1,
			Path:       sc.chunk.docRelPath,
			Title:      sc.chunk.heading,
			Snippet:    firstRunes(sc.chunk.text, qnaSnippetRunes),
			Heading:    sc.chunk.heading,
			ChunkIndex: sc.chunk.index,
		})
		fmt.Fprintf(&ctx, "[片段 %d] 来源：%s（章节：%s）\n%s\n\n",
			len(citations), sc.chunk.docRelPath, sc.chunk.heading, string(text))
		total += len(text)
		if total >= qnaMaxTotalRunes {
			break
		}
	}

	if len(citations) == 0 {
		// 知识库中未检索到相关内容，直接返回提示而不调用 LLM
		return &QnAResponse{
			Answer:    "未在当前知识库中检索到与该问题相关的内容。请尝试换个问法，或先补充相关笔记。",
			Citations: nil,
		}, nil
	}

	systemPrompt := "你是个人知识库问答助手。请仅依据下面提供的知识库片段回答用户问题，" +
		"并遵循以下规则：\n" +
		"1. 回答使用与用户问题相同的语言；\n" +
		"2. 在引用某个片段的位置标注 [n]（n 为片段编号），例如 [1] [2]；\n" +
		"3. 如果片段中没有足够信息回答问题，请如实说明知识库中缺少相关内容；\n" +
		"4. 使用 Markdown 输出，简洁准确，不要编造片段中不存在的内容。\n\n" +
		"知识库片段：\n" + ctx.String()

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

	baseURL = normalizeBaseURL(baseURL)
	if model == "" {
		model = "gpt-4o-mini"
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, credential)

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

// ---------------------------------------------------------------------------
// P1-3 混合检索（BM25 leg + 向量 leg，RRF 融合）
// ---------------------------------------------------------------------------

// chunkKey 是块级融合的去重键（文档相对路径 + 块序号）。
func chunkKey(relPath string, idx int) string {
	return relPath + "#" + strconv.Itoa(idx)
}

// rrf 计算 RRF 的单一贡献值：1/(k+rank)，rank 从 1 开始。
func rrf(rank, k int) float64 {
	return 1.0 / float64(k+rank)
}

// ensureVectorStore 返回（按需构建/加载）工作区对应的向量索引。
//
// 缓存按「工作区 + 模型」键；首次会同步全库 embed（较重，但持久化后后续秒加载）。
// 未配置 embedding（embedder 为 nil 或模型名为空）或 fileService 缺失时返回错误，
// 由调用方据此退化纯 BM25，不阻断问答。
func (s *QnAService) ensureVectorStore(workspacePath string, cfg EmbeddingConfig) (*VectorStore, error) {
	if s.embedder == nil || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("未配置 embedding，向量 leg 不可用")
	}
	if s.fileService == nil {
		return nil, fmt.Errorf("缺少文件服务，无法构建向量索引")
	}
	key := workspacePath + "|" + strings.TrimSpace(cfg.Model)
	s.vectorMu.Lock()
	defer s.vectorMu.Unlock()
	if vs, ok := s.vectorCache[key]; ok {
		return vs, nil
	}
	persistDir := persistDirForWorkspace(workspacePath, cfg.Model)
	vs, err := NewVectorStore(persistDir, s.embedder, cfg)
	if err != nil {
		return nil, err
	}
	if err := vs.Build(context.Background(), workspacePath, s.fileService, s.embedder, cfg); err != nil {
		return nil, err
	}
	s.vectorCache[key] = vs
	return vs, nil
}

// retrieveChunksHybrid 在块级融合 BM25 与向量检索（RRF 0.4/0.6）。
//
// 未配置 embedding（cfg.Model 为空或 embedder 为 nil）时直接返回纯 BM25 块结果，
// 保证与 P0 结束态逐字节一致（红线 #1）。向量 leg 构建/查询失败时同样降级，
// 绝不因语义增强导致功能残废。
//
// 配置 rerank（rerankCfg 非空且 reranker 可用）时，在 RRF 融合后再做一遍重排序
// （宽候选池 → cross-encoder 重排 → Top-K），进一步修正召回顺序。rerank 不可用
// 时静默回退到纯 RRF 截断，行为与 P1-3 结束态逐字节一致。
func (s *QnAService) retrieveChunksHybrid(workspacePath, question string, embCfg EmbeddingConfig, rerankCfg RerankConfig) []scoredChunk {
	bm25 := s.retrieveChunks(workspacePath, question)
	if s.embedder == nil || strings.TrimSpace(embCfg.Model) == "" {
		return bm25
	}
	vs, err := s.ensureVectorStore(workspacePath, embCfg)
	if err != nil || vs == nil {
		return bm25
	}
	hits, err := vs.Search(context.Background(), question, qnaCandidateDocs)
	if err != nil || len(hits) == 0 {
		return bm25
	}

	const rrfK = 60
	bm25Rank := make(map[string]int)
	for i, sc := range bm25 {
		bm25Rank[chunkKey(sc.chunk.docRelPath, sc.chunk.index)] = i + 1
	}
	vecRank := make(map[string]int)
	for i, h := range hits {
		vecRank[chunkKey(h.RelPath, h.ChunkIndex)] = i + 1
	}

	seen := make(map[string]bool)
	type fused struct {
		key   string
		score float64
	}
	var fusedList []fused
	add := func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		var score float64
		if r, ok := bm25Rank[key]; ok {
			score += 0.4 * rrf(r, rrfK)
		}
		if r, ok := vecRank[key]; ok {
			score += 0.6 * rrf(r, rrfK)
		}
		fusedList = append(fusedList, fused{key: key, score: score})
	}
	for key := range bm25Rank {
		add(key)
	}
	for key := range vecRank {
		add(key)
	}
	sort.Slice(fusedList, func(i, j int) bool {
		if fusedList[i].score != fusedList[j].score {
			return fusedList[i].score > fusedList[j].score
		}
		return fusedList[i].key < fusedList[j].key
	})

	bm25ByKey := make(map[string]scoredChunk, len(bm25))
	for _, sc := range bm25 {
		bm25ByKey[chunkKey(sc.chunk.docRelPath, sc.chunk.index)] = sc
	}
	vecByKey := make(map[string]vectorHit, len(hits))
	for _, h := range hits {
		vecByKey[chunkKey(h.RelPath, h.ChunkIndex)] = h
	}

	// 解析融合排名为候选块（按 RRF 分数降序，先不截断——保留全量供重排宽召回）。
	candidates := make([]scoredChunk, 0, len(fusedList))
	for _, f := range fusedList {
		if sc, ok := bm25ByKey[f.key]; ok {
			candidates = append(candidates, sc)
		} else if h, ok := vecByKey[f.key]; ok {
			candidates = append(candidates, scoredChunk{
				chunk: chunk{
					docRelPath: h.RelPath,
					heading:    h.Heading,
					index:      h.ChunkIndex,
					text:       h.Content,
				},
				score: float64(h.Similarity),
			})
		}
	}
	if len(candidates) == 0 {
		return bm25
	}

	// P1-3b：配置 rerank 且服务可用时，取宽候选池交重排，再取 Top-K 进上下文。
	// 重排失败（未配置 / 服务不可用 / 返回空）静默回退 RRF 截断，绝不因此残废问答。
	if s.reranker != nil {
		pool := candidates
		if len(pool) > qnaRerankPool {
			pool = pool[:qnaRerankPool]
		}
		docs := make([]string, len(pool))
		for i, c := range pool {
			docs[i] = c.chunk.text
		}
		if ranked, rErr := s.reranker.Rerank(context.Background(), rerankCfg, question, docs); rErr == nil && len(ranked) > 0 {
			out := make([]scoredChunk, 0, qnaTopKChunks)
			for _, r := range ranked {
				if r.Index < 0 || r.Index >= len(pool) {
					continue
				}
				out = append(out, pool[r.Index])
				if len(out) >= qnaTopKChunks {
					break
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	// 未配置 rerank 或重排回退：按 RRF 分数取 Top-K（与 P1-3 结束态逐字节一致）。
	if len(candidates) > qnaTopKChunks {
		candidates = candidates[:qnaTopKChunks]
	}
	return candidates
}

// HybridSearch 是文档级混合检索，供前端搜索页语义增强使用（P1-3，P1-3b 增强）。
//
// 融合 BM25 文档级排名与向量文档级排名（每个文档取最高块相似度），RRF 加权。
// 未配置 embedding 时退化为纯 BM25 文档级检索，返回结构与 SearchService.Search 一致。
//
// P1-3b：额外配置 rerank（rerankCfg 非空且 reranker 可用）时，在 RRF 融合后
// 再用 cross-encoder 对候选文档做一遍重排，修正排序。rerank 不可用静默回退 RRF，
// 行为与 P1-3 结束态一致。
func (s *QnAService) HybridSearch(workspacePath, query, embBaseURL, embModel, embAPIKey string, rerankCfg RerankConfig) ([]*SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*SearchResult{}, nil
	}
	idx := s.idxOf(workspacePath)
	if _, err := idx.refresh(workspacePath); err != nil {
		return nil, err
	}
	queryLower := strings.ToLower(query)
	queryTokens := tokenize(queryLower)
	if len(queryTokens) == 0 {
		return []*SearchResult{}, nil
	}

	idx.mu.RLock()
	docScores := idx.scoreBM25(queryTokens, defaultMaxSearchResults)
	idx.mu.RUnlock()

	const rrfK = 60
	bm25Rank := make(map[string]int, len(docScores))
	for i, sd := range docScores {
		bm25Rank[sd.doc.relPath] = i + 1
	}

	// 向量文档级 rank：聚合每个文档的最高块相似度，再按相似度降序排名
	vecRank := make(map[string]int)
	if s.embedder != nil && strings.TrimSpace(embModel) != "" {
		embCfg := EmbeddingConfig{BaseURL: embBaseURL, Model: embModel, APIKey: embAPIKey}
		if vs, err := s.ensureVectorStore(workspacePath, embCfg); err == nil && vs != nil {
			if hits, qErr := vs.Search(context.Background(), query, defaultMaxSearchResults*2); qErr == nil {
				docSim := make(map[string]float32)
				for _, h := range hits {
					if s2, ok := docSim[h.RelPath]; !ok || h.Similarity > s2 {
						docSim[h.RelPath] = h.Similarity
					}
				}
				type ds struct {
					rel string
					sim float32
				}
				dlist := make([]ds, 0, len(docSim))
				for rel, sim := range docSim {
					dlist = append(dlist, ds{rel: rel, sim: sim})
				}
				sort.Slice(dlist, func(i, j int) bool { return dlist[i].sim > dlist[j].sim })
				for i, d := range dlist {
					vecRank[d.rel] = i + 1
				}
			}
		}
	}

	// RRF 融合文档级
	docScore := make(map[string]float64, len(bm25Rank)+len(vecRank))
	for rel, r := range bm25Rank {
		docScore[rel] += 0.4 * rrf(r, rrfK)
	}
	for rel, r := range vecRank {
		docScore[rel] += 0.6 * rrf(r, rrfK)
	}
	type fr struct {
		relPath string
		score   float64
	}
	fused := make([]fr, 0, len(docScore))
	for rel, sc := range docScore {
		fused = append(fused, fr{relPath: rel, score: sc})
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].score > fused[j].score })

	// docByRel 加速「relPath → 文档」查找，避免原实现每轮线性扫描 docScores。
	docByRel := make(map[string]*cachedDoc, len(docScores))
	for _, sd := range docScores {
		docByRel[sd.doc.relPath] = sd.doc
	}

	// P1-3b：配置 rerank 且可用时，对候选文档做一遍重排，重排后的顺序覆盖 RRF 顺序。
	if s.reranker != nil {
		texts := make([]string, 0, len(fused))
		for _, f := range fused {
			doc, ok := docByRel[f.relPath]
			if !ok {
				texts = append(texts, "")
				continue
			}
			content, _, err := doc.contentOnce(idx)
			if err != nil {
				texts = append(texts, "")
				continue
			}
			r := []rune(content)
			if len(r) > 2000 {
				r = r[:2000]
			}
			texts = append(texts, string(r))
		}
		if ranked, rErr := s.reranker.Rerank(context.Background(), rerankCfg, query, texts); rErr == nil && len(ranked) > 0 {
			reranked := make([]fr, 0, len(ranked))
			for _, rk := range ranked {
				if rk.Index < 0 || rk.Index >= len(fused) {
					continue
				}
				reranked = append(reranked, fused[rk.Index])
			}
			if len(reranked) > 0 {
				fused = reranked
			}
		}
	}

	// 两段式 snippet（与 SearchService.Search 同口径）：前 defaultEagerSnippetLimit
	// 条即时生成，其余留空交前端按需取，避免给 Top-N 全部读正文拖慢首屏。
	results := make([]*SearchResult, 0, len(fused))
	for i, f := range fused {
		doc, ok := docByRel[f.relPath]
		if !ok {
			continue
		}
		var snippet string
		var matchCount int
		if i < defaultEagerSnippetLimit {
			content, contentLower, err := doc.contentOnce(idx)
			if err != nil {
				continue
			}
			matchCount = strings.Count(contentLower, queryLower)
			if matchCount == 0 {
				for _, qt := range queryTokens {
					matchCount += doc.tokenFreq[qt]
				}
			}
			snippet = extractSnippet(content, queryTokens[0], 50)
		} else {
			for _, qt := range queryTokens {
				matchCount += doc.tokenFreq[qt]
			}
		}
		results = append(results, &SearchResult{
			Path:       doc.relPath,
			Title:      doc.title,
			Snippet:    snippet,
			MatchCount: matchCount,
			ModTime:    doc.modTime,
		})
	}
	return results, nil
}
