package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 蓝图专项 5：LLM-as-a-Reranker 内存自愈。
//
// 背景：本地只部署了普通对话模型（如 Qwen-2.5-7B）时，没有
// /rerank 专有端点，重排 API 不可用。这里用一次对话调用对
// 初筛候选块做相关性打分（1~5），在内存中重排，摆脱对上游
// 专有重排 API 的依赖。打分失败（解析失败/超时）静默降级为
// 原始顺序，绝不阻断问答。

// LLMEndpoint 对话模型端点（Answer 每次调用传入的凭据快照）
type LLMEndpoint struct {
	apiKey   string
	baseURL  string
	model    string
	protocol string
}

func (e LLMEndpoint) hasCreds() bool {
	return strings.TrimSpace(e.baseURL) != "" && strings.TrimSpace(e.model) != ""
}

// llmChatFunc 与 qnaservice 的 llmChatComplete 同签名，测试可注入假实现。
type llmChatFunc func(ctx context.Context, apiKey, baseURL, model, protocol, systemPrompt, userPrompt string) (string, error)

// rerankPromptSystem 打分器系统提示词
const rerankPromptSystem = "你是检索相关性打分器。给定用户问题和若干文档片段，" +
	"请对每个片段与问题的相关性打分（1=完全不相关，5=高度相关）。" +
	"只输出 JSON 数组，格式：[{\"i\":1,\"score\":5},...]，不要输出其他内容。"

// llmRerankByPrompt 用对话模型对 docs 打分，返回按相关性降序的原始下标。
// 解析失败或 LLM 不可用时返回 nil（调用方回退原始顺序）。
func llmRerankByPrompt(
	ctx context.Context,
	chat llmChatFunc,
	apiKey, baseURL, model, protocol, question string,
	docs []string,
) []int {
	if len(docs) == 0 {
		return nil
	}
	var sb strings.Builder
	for i, doc := range docs {
		preview := []rune(doc)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, string(preview))
	}
	userPrompt := "问题：" + question + "\n\n文档片段：\n" + sb.String()

	raw, err := chat(ctx, apiKey, baseURL, model, protocol, rerankPromptSystem, userPrompt)
	if err != nil {
		return nil
	}
	scores, perr := parseRerankScores(raw, len(docs))
	if perr != nil || len(scores) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(docs))
	for i := range docs {
		indexes = append(indexes, i)
	}
	scoreOf := func(i int) int { return scores[i] }
	sort.SliceStable(indexes, func(a, b int) bool {
		return scoreOf(indexes[a]) > scoreOf(indexes[b])
	})
	// 全 0 分（模型没理解任务）视为失败，回退原始顺序
	if scoreOf(indexes[0]) <= 0 {
		return nil
	}
	return indexes
}

// parseRerankScores 宽容解析打分输出：优先 JSON 数组，失败再按行 "i: score"。
// 未提及的片段记 0 分。
func parseRerankScores(raw string, docCount int) (map[int]int, error) {
	type scoreEntry struct {
		I     int `json:"i"`
		Score int `json:"score"`
	}
	scores := make(map[int]int, docCount)

	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			var entries []scoreEntry
			if err := json.Unmarshal([]byte(raw[start:end+1]), &entries); err == nil {
				for _, e := range entries {
					if e.I >= 1 && e.I <= docCount {
						scores[e.I-1] = e.Score
					}
				}
				return scores, nil
			}
		}
	}
	// 行级兜底："1: 4" / "1 - 4" / "1 分"
	for _, line := range strings.Split(raw, "\n") {
		var idx, score int
		if n, _ := fmt.Sscanf(strings.TrimSpace(line), "%d:%d", &idx, &score); n == 2 {
			if idx >= 1 && idx <= docCount {
				scores[idx-1] = score
			}
		}
	}
	if len(scores) == 0 {
		return nil, fmt.Errorf("无法解析打分输出")
	}
	return scores, nil
}
