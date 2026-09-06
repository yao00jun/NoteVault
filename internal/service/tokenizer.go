package service

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

// 蓝图专项 4：纯 Go gse 中文分词（嵌入式核心词典，零 CGO）。
//
// 切分策略（拉丁与 CJK 分治）：
//   - 拉丁/数字/下划线连续串整体成一个 token（与旧 tokenize 一致，
//     gse 对未知英文会按单字切，必须自行处理）；
//   - CJK 连续段交给 gse：索引侧 CutAll 细粒度全切分（「分布式锁」→
//     分布/分布式/布式/锁），查询侧 CutSearch 常规切分（「缓存失效」→
//     缓存/失效）。两侧共用同一嵌入式词典保证一致；
//   - gse 初始化失败（理论上不会）整体回退 bigram tokenize，两侧一致降级。
//
// 对比旧 bigram：词典切分让「分布式锁」作为整词参与 BM25 打分，
// 复合词召回精度显著更好（bigram 只能按二元命中）。

var (
	tokenizerOnce sync.Once
	gseSegmenter  gse.Segmenter
	gseReady      bool
)

func initGseSegmenter() {
	tokenizerOnce.Do(func() {
		seg, err := gse.NewEmbed("zh")
		if err != nil {
			// 嵌入式词典理论上不会失败；万一失败，退回 bigram 兜底
			return
		}
		gseSegmenter = seg
		gseReady = true
	})
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// filterGseTokens 清洗 gse 输出：去空白/纯标点 token
func filterGseTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || !hasLetterOrDigit(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// segmentRuns 把文本拆成 拉丁run / CJKrun / 分隔符 三类连续段
type textRun struct {
	latin bool
	text  string
}

func segmentRuns(s string) []textRun {
	var runs []textRun
	var latin strings.Builder
	var han strings.Builder

	flushLatin := func() {
		if latin.Len() > 0 {
			runs = append(runs, textRun{latin: true, text: latin.String()})
			latin.Reset()
		}
	}
	flushHan := func() {
		if han.Len() > 0 {
			runs = append(runs, textRun{latin: false, text: han.String()})
			han.Reset()
		}
	}

	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			flushHan()
			latin.WriteRune(unicode.ToLower(r))
		default:
			flushLatin()
			flushHan()
		}
	}
	flushLatin()
	flushHan()
	return runs
}

// tokenizeIndex 索引侧分词：CJK 细粒度全切分（CutAll）
func tokenizeIndex(s string) []string {
	initGseSegmenter()
	if !gseReady {
		return tokenize(s)
	}
	out := make([]string, 0, len(s)/4)
	for _, run := range segmentRuns(strings.ToLower(s)) {
		if run.latin {
			out = append(out, run.text)
		} else {
			out = append(out, filterGseTokens(gseSegmenter.CutAll(run.text))...)
		}
	}
	return out
}

// tokenizeQuery 查询侧分词：与索引侧同 token 空间（保证召回）。
// 若查询侧用 CutSearch、索引侧用 CutAll，词典切分差异会造成
// 查询 token 在索引里不存在（如单字）而漏召回——这里直接复用
// 索引侧切分，BM25 按多 token 累计打分不受影响。
func tokenizeQuery(s string) []string {
	return tokenizeIndex(s)
}
