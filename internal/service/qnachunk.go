package service

import (
	"math"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// 文本分块（P0-2）
//
// 旧实现把命中的「整篇文档」截断到前 2500 字符注入上下文，有两个问题：
//  1. 长笔记的答案如果落在中后段，模型根本看不到——它被截掉了；
//  2. 短笔记只有几百字，却照样占用 2500 字的预算额度。
// 分块后按块召回，上下文预算就能花在真正相关的片段上。
// ---------------------------------------------------------------------------

// chunk 是文档的一个片段，QnA 检索与上下文注入的最小单位。
type chunk struct {
	docRelPath string // 所属文档的相对路径
	heading    string // 所属最近的上级标题（无标题时为文档标题）
	index      int    // 在文档内的序号，从 0 开始
	text       string
}

// scoredChunk 块 + 相关性得分
type scoredChunk struct {
	chunk chunk
	score float64
}

// tailRunes 返回字符串末尾 n 个 rune（n 超出长度时返回整个串）
func tailRunes(s string, n int) string {
	r := []rune(s)
	if n >= len(r) {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(r[len(r)-n:])
}

// firstRunes 返回字符串开头 n 个 rune，超出部分用省略号表示
func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// chunkDocument 把一篇文档切成若干片段。
//
// 切分策略（越靠前优先级越高）：
//  1. Markdown 标题（# / ## / ###）——标题天然是语义边界，
//     按它切出来的块往往是「一个完整的论点」；
//  2. 块内累积到 maxLen 时结算，并把尾部 overlap 个字符带入下一块，
//     避免答案正好被切断在边界上；
//  3. 结尾剩余内容单独成块。
//
// heading 取当前块之前最近一次出现的标题；文档开头没有标题时回落到文档标题。
func chunkDocument(relPath, title, content string, maxLen, overlap int) []chunk {
	if maxLen <= 0 {
		maxLen = qnaChunkMaxRunes
	}
	if overlap < 0 {
		overlap = 0
	}

	var chunks []chunk
	var buf strings.Builder
	heading := title
	index := 0

	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		chunks = append(chunks, chunk{
			docRelPath: relPath,
			heading:    heading,
			index:      index,
			text:       text,
		})
		index++
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// 标题行：先结算当前块，再切换 heading，标题本身带入新块
		// （否则块失去上下文，模型看到一段没有主题的文字）
		if strings.HasPrefix(trimmed, "#") {
			flush()
			if h := strings.TrimLeft(trimmed, "# "); h != "" {
				heading = h
			}
			buf.WriteString(trimmed)
			buf.WriteString("\n")
			continue
		}

		buf.WriteString(line)
		buf.WriteString("\n")

		if len([]rune(buf.String())) >= maxLen {
			text := buf.String()
			// 结算完整块
			chunks = append(chunks, chunk{
				docRelPath: relPath,
				heading:    heading,
				index:      index,
				text:       strings.TrimSpace(text),
			})
			index++
			// 尾部重叠带入下一块，让跨边界的答案不至于整段丢失
			buf.Reset()
			buf.WriteString(tailRunes(text, overlap))
		}
	}
	flush()

	return chunks
}

// scoreChunks 对切分出的块做 BM25 打分。
//
// 不复用 idx.scoreBM25：那是针对已建索引文档的（依赖倒排表与 per-doc 统计），
// 而块是运行时临时切出来的，没有倒排表。
//
// IDF 仍然取索引的真实 df——它反映整个语料的词分布，比在几十个块上
// 临时统计可靠得多；tf 与长度则取块自身。
func scoreChunks(chunks []chunk, queryTokens []string, idx *searchIndex) []scoredChunk {
	if len(chunks) == 0 || len(queryTokens) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	if n < 1 {
		n = 1
	}

	avgdl := 0.0
	for _, c := range chunks {
		avgdl += float64(len([]rune(c.text)))
	}
	avgdl /= float64(len(chunks))
	if avgdl < 1 {
		avgdl = 1
	}

	scored := make([]scoredChunk, 0, len(chunks))
	for _, c := range chunks {
		tokens := tokenize(c.text)
		freq := make(map[string]int, len(tokens))
		for _, t := range tokens {
			freq[t]++
		}
		dl := float64(len(tokens))
		if dl < 1 {
			dl = 1
		}

		var score float64
		for _, qt := range queryTokens {
			tf := float64(freq[qt])
			if tf == 0 {
				continue
			}
			df := float64(len(idx.inverted[qt]))
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*dl/avgdl))
		}
		if score > 0 {
			scored = append(scored, scoredChunk{chunk: c, score: score})
		}
	}

	// 分数相同时按（文档路径, 块序号）排序，保证结果稳定
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].chunk.docRelPath != scored[j].chunk.docRelPath {
			return scored[i].chunk.docRelPath < scored[j].chunk.docRelPath
		}
		return scored[i].chunk.index < scored[j].chunk.index
	})
	return scored
}
