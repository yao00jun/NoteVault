package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newQnATestWorkspace 创建包含若干 Markdown 文档的临时工作区
func newQnATestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go-basics.md":  "# Go 基础\n\nGo 是一门静态类型的编译型语言，由 Google 开发。Go 支持协程 goroutine 和通道 channel 进行并发编程。",
		"deploy.md":     "# 部署指南\n\n部署流程：先执行 go build 构建二进制，再使用 systemd 托管进程，最后配置 nginx 反向代理。",
		"shopping.md":   "# 购物清单\n\n牛奶、鸡蛋、面包、咖啡豆。",
		"notes/term.md": "# 术语表\n\ngoroutine：Go 语言中的轻量级线程。channel：goroutine 之间通信的管道。",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestQnAService_Answer(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	var gotBody openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Go 支持 goroutine 并发 [1]。"}}]}`))
	}))
	defer server.Close()

	svc := NewQnAService()
	// 末尾四个空字符串依次为 protocol / embedding 配置（未配置 → 退化为纯 BM25，行为不变）
	resp, err := svc.Answer("test-key", server.URL, "model-x", "", "", "", "", RerankConfig{}, ws, "Go 语言如何做并发编程")
	if err != nil {
		t.Fatalf("Answer failed: %v", err)
	}
	if !strings.Contains(resp.Answer, "goroutine") {
		t.Errorf("unexpected answer: %s", resp.Answer)
	}
	if len(resp.Citations) == 0 {
		t.Fatalf("expected citations, got none")
	}
	// 检索应命中 Go 相关文档（go-basics.md / notes/term.md），而非购物清单
	paths := make(map[string]bool)
	for _, c := range resp.Citations {
		paths[c.Path] = true
	}
	if !paths["go-basics.md"] {
		t.Errorf("expected go-basics.md in citations, got %v", paths)
	}
	if paths["shopping.md"] {
		t.Errorf("shopping.md should not be cited: %v", paths)
	}
	// 请求中 system prompt 应包含检索到的文档内容
	if !strings.Contains(gotBody.Messages[0].Content, "goroutine") {
		t.Errorf("system prompt should contain retrieved context")
	}
	if gotBody.Messages[1].Content != "Go 语言如何做并发编程" {
		t.Errorf("user message should be the question, got %s", gotBody.Messages[1].Content)
	}
}

func TestQnAService_Answer_NoKey(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("", "http://x", "m", "", "", "", "", RerankConfig{}, "/tmp", "问题")
	if err == nil {
		t.Errorf("expected error for empty key")
	}
}

func TestQnAService_Answer_EmptyQuestion(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("k", "http://x", "m", "", "", "", "", RerankConfig{}, "/tmp", "   ")
	if err == nil {
		t.Errorf("expected error for empty question")
	}
}

func TestQnAService_Answer_NoWorkspace(t *testing.T) {
	svc := NewQnAService()
	_, err := svc.Answer("k", "http://x", "m", "", "", "", "", RerankConfig{}, "", "问题")
	if err == nil {
		t.Errorf("expected error for empty workspace")
	}
}

func TestQnAService_Answer_NoMatch(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"}}]}`))
	}))
	defer server.Close()

	// 量子物理相关 token 不存在于任何文档，应直接返回提示且不调用 LLM
	svc := NewQnAService()
	resp, err := svc.Answer("k", server.URL, "m", "", "", "", "", RerankConfig{}, ws, "quantum entanglement physics")
	if err != nil {
		t.Fatalf("Answer failed: %v", err)
	}
	if called {
		t.Errorf("LLM should not be called when no docs match")
	}
	if len(resp.Citations) != 0 {
		t.Errorf("expected no citations, got %d", len(resp.Citations))
	}
	if resp.Answer == "" {
		t.Errorf("expected non-empty fallback answer")
	}
}

func TestQnAService_Answer_APIError(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	svc := NewQnAService()
	_, err := svc.Answer("k", server.URL, "m", "", "", "", "", RerankConfig{}, ws, "Go 并发")
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("expected API error, got %v", err)
	}
}

// TestQnAService_RetrieveChunks_RanksRelevantFirst 验证块级检索的相关性排序。
//
// 旧实现按「标题命中 +2、正文命中 +1」的整数打分，没有 IDF 也没有长度归一。
// 现在走 BM25，deploy.md 因标题与正文都命中「部署 deploy」应当排在最前。
func TestQnAService_RetrieveChunks_RanksRelevantFirst(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	svc := NewQnAService()

	got := svc.retrieveChunks(ws, "部署 deploy 流程")
	if len(got) == 0 {
		t.Fatalf("expected retrieved chunks")
	}
	if got[0].chunk.docRelPath != "deploy.md" {
		t.Errorf("expected deploy.md ranked first, got %s", got[0].chunk.docRelPath)
	}
}

// TestQnAService_RetrieveChunks_RespectsTopK 块数上限必须生效。
// 上限是社区实测的甜点：超过 8 块后模型会被冗余信息干扰。
func TestQnAService_RetrieveChunks_RespectsTopK(t *testing.T) {
	ClearAllSearchIndexes()
	dir := t.TempDir()
	// 造 30 篇都含「缓存」的文档，足以超过 qnaTopKChunks
	for i := 0; i < 30; i++ {
		name := filepath.Join(dir, "note-"+string(rune('a'+i%26))+string(rune('0'+i/26))+".md")
		body := "# 缓存\n缓存失效的若干种处理方式，这里讨论第 " + string(rune('A'+i%26)) + " 种。\n"
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewQnAService()
	got := svc.retrieveChunks(dir, "缓存失效")
	if len(got) > qnaTopKChunks {
		t.Errorf("expected at most %d chunks, got %d", qnaTopKChunks, len(got))
	}
	if len(got) == 0 {
		t.Fatalf("expected some chunks")
	}
}

// TestChunkDocument_HeadingAndOverlap 分块要按标题切、并保留相邻块重叠。
func TestChunkDocument_HeadingAndOverlap(t *testing.T) {
	content := "# 标题一\n" + strings.Repeat("甲", 400) + "\n## 标题二\n" + strings.Repeat("乙", 400)
	chunks := chunkDocument("a.md", "文档标题", content, 300, 50)

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
	// heading 字段必须记录每块所属章节——块文本本身可能不含标题
	// （overlap 只带入尾部文字），章节信息靠 heading 字段在上下文里补上
	for i, c := range chunks {
		if c.heading == "" {
			t.Errorf("chunk %d has empty heading", i)
		}
		if c.docRelPath != "a.md" {
			t.Errorf("chunk %d wrong path: %s", i, c.docRelPath)
		}
	}
	if chunks[0].heading != "标题一" {
		t.Errorf("first chunk should belong to 标题一, got %q", chunks[0].heading)
	}

	// 标题切换应当产生一个以新标题开头的新块
	found := false
	for _, c := range chunks {
		if strings.HasPrefix(c.text, "## 标题二") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected some chunk starting with ## 标题二, got %d chunks", len(chunks))
	}
}

// TestChunkDocument_ShortDoc 短文档应当只产生一个块，不被无谓切碎。
func TestChunkDocument_ShortDoc(t *testing.T) {
	chunks := chunkDocument("a.md", "标题", "# 短\n就一句话。", 700, 70)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for a short doc, got %d", len(chunks))
	}
}

// ---------------------------------------------------------------------------
// P1-3 混合检索测试
// ---------------------------------------------------------------------------

// tokenEmbedder 是确定性测试 embedding：把 token 哈希到固定维度向量的桶里。
// 同主题文本（共享 token）向量相近，足以验证向量 leg 的「能召回相关块」「持久化」
// 「降级」等逻辑。真实语义召回（跨语言同义）由集成测试覆盖。
type tokenEmbedder struct{ dim int }

func (e tokenEmbedder) Embed(ctx context.Context, cfg EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, e.dim)
		for _, tok := range tokenize(strings.ToLower(t)) {
			h := 0
			for _, c := range tok {
				h = (h*31 + int(c)) % e.dim
			}
			v[h] += 1
		}
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		if sum > 0 {
			scale := float32(1 / math.Sqrt(sum))
			for j := range v {
				v[j] *= scale
			}
		}
		out[i] = v
	}
	return out, nil
}

// TestVectorStore_BuildAndSearch 验证向量索引的构建、查询与持久化。
func TestVectorStore_BuildAndSearch(t *testing.T) {
	ws := newQnATestWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())
	emb := tokenEmbedder{dim: 256}
	cfg := EmbeddingConfig{BaseURL: "http://localhost:11434/v1", Model: "test-model"}

	dir1 := persistDirForWorkspace(ws, cfg.Model)
	vs, err := NewVectorStore(dir1, emb, cfg)
	if err != nil {
		t.Fatalf("NewVectorStore failed: %v", err)
	}
	if err := vs.Build(context.Background(), ws, fs, emb, cfg); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if vs.Count() == 0 {
		t.Fatal("expected vectors built")
	}

	hits, err := vs.Search(context.Background(), "Go 并发 goroutine", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	for _, h := range hits {
		if h.RelPath == "" {
			t.Errorf("hit missing relPath")
		}
	}

	// 持久化：新建 VectorStore 加载同一目录，Count 应 > 0（无需重建）
	vs2, err := NewVectorStore(dir1, emb, cfg)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if vs2.Count() == 0 {
		t.Fatal("expected loaded vectors from disk")
	}

	// 换模型 → 不同目录，首次 Count 为 0（待构建，不污染旧索引）
	dir2 := persistDirForWorkspace(ws, "other-model")
	vs3, err := NewVectorStore(dir2, emb, cfg)
	if err != nil {
		t.Fatalf("new-model store failed: %v", err)
	}
	if vs3.Count() != 0 {
		t.Fatalf("expected empty store for new model, got %d", vs3.Count())
	}
}

// TestQnAService_HybridSearch_DegradesToBM25 未配置 embedding 时退化为纯 BM25，
// 仍应命中相关文档、排除无关文档。
func TestQnAService_HybridSearch_DegradesToBM25(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	svc := NewQnAService()
	// 末尾三个空字符串为 embedding 配置（未配置 → 纯 BM25）
	res, err := svc.HybridSearch(ws, "Go 并发编程 goroutine", "", "", "", RerankConfig{})
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("expected BM25 results")
	}
	paths := make(map[string]bool)
	for _, r := range res {
		paths[r.Path] = true
	}
	if !paths["go-basics.md"] {
		t.Errorf("expected go-basics.md, got %v", paths)
	}
	if paths["shopping.md"] {
		t.Errorf("shopping.md should not appear, got %v", paths)
	}
}

// TestQnAService_RetrieveChunksHybrid_DegradesToBM25 降级时融合层必须与纯 BM25
// 逐块一致（红线 #1：未配置 embedding 时行为与 P0 结束态逐字节一致）。
func TestQnAService_RetrieveChunksHybrid_DegradesToBM25(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	svc := NewQnAService()
	plain := svc.retrieveChunks(ws, "部署 deploy 流程")
	hybrid := svc.retrieveChunksHybrid(ws, "部署 deploy 流程", EmbeddingConfig{}, RerankConfig{})
	if len(plain) != len(hybrid) {
		t.Fatalf("length mismatch: %d vs %d", len(plain), len(hybrid))
	}
	for i := range plain {
		if plain[i].chunk.docRelPath != hybrid[i].chunk.docRelPath ||
			plain[i].chunk.index != hybrid[i].chunk.index {
			t.Errorf("chunk %d differs: %s#%d vs %s#%d",
				i, plain[i].chunk.docRelPath, plain[i].chunk.index,
				hybrid[i].chunk.docRelPath, hybrid[i].chunk.index)
		}
	}
}

// TestQnAService_SemanticRecall_RequiresOllama 是集成测试，验证真实 bge-m3 下
// 语义召回（跨语言同义）相对纯 BM25 的提升。默认跳过，需本地 Ollama + bge-m3：
//   NV_OLLAMA_TEST=1 go test ./internal/service/ -run SemanticRecall
//
// 2026-09-02 修正两处使本用例形同虚设的问题：
//  1. embBaseURL 原传空串，normalizeBaseURL 会把它补成 https://api.openai.com/v1，
//     即便本机跑着 Ollama，请求也会打到 OpenAI 并因缺少 API Key 而失败；
//  2. 语料原用 newQnATestWorkspace，其中根本没有「缓存」相关文档，
//     断言永远不可能成立。改用评测语料 newEvalWorkspace（含 cache-invalidation.md）。
//
// 完整口径（Recall@5 / MRR@5）见 TestRetrievalEval_Ollama_BGEM3。
func TestQnAService_SemanticRecall_RequiresOllama(t *testing.T) {
	if os.Getenv("NV_OLLAMA_TEST") == "" {
		t.Skip("set NV_OLLAMA_TEST=1 with Ollama + bge-m3 running to exercise real semantic recall")
	}
	ClearAllSearchIndexes()
	ws := newEvalWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())
	svc := NewQnAServiceWithRegistry(fs, nil, NewOllamaEmbeddingClient(), nil)

	// 中文笔记里写「缓存失效」，用英文同义 query 应被向量 leg 召回
	res, err := svc.HybridSearch(ws, "how to invalidate cache",
		envOrDefault("NV_EVAL_EMBED_BASE", "http://127.0.0.1:11434/v1"),
		envOrDefault("NV_EVAL_EMBED_MODEL", "bge-m3"), "", RerankConfig{})
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}
	foundCache := false
	for _, r := range res {
		if strings.Contains(strings.ToLower(r.Path+r.Title+r.Snippet), "缓存") {
			foundCache = true
			break
		}
	}
	if !foundCache {
		t.Errorf("semantic query should recall cache-related note, got %v", res)
	}
}

// ---------------------------------------------------------------------------
// P1-3b Rerank 测试
// ---------------------------------------------------------------------------

// fakeReranker 是确定性测试重排器。
//   - err=true 时返回错误（模拟服务不可用，触发降级）
//   - reverse=true 时把候选顺序完全反转（用于验证「重排确实改变了召回顺序」）
//   - results 非空时原样返回（精确控制输出顺序）
type fakeReranker struct {
	results []RerankResult
	reverse bool
	err     bool
}

func (f *fakeReranker) Rerank(ctx context.Context, cfg RerankConfig, query string, documents []string) ([]RerankResult, error) {
	if f.err {
		return nil, fmt.Errorf("fake rerank error")
	}
	if f.results != nil {
		return f.results, nil
	}
	n := len(documents)
	out := make([]RerankResult, n)
	if f.reverse {
		for i := 0; i < n; i++ {
			out[n-1-i] = RerankResult{Index: i, Score: float64(i)}
		}
		return out, nil
	}
	for i := 0; i < n; i++ {
		out[i] = RerankResult{Index: i, Score: float64(n - i)}
	}
	return out, nil
}

// TestReranker_CohereRerank_NoKey 验证本机兼容 /rerank 端点无需鉴权即可解析响应。
func TestReranker_CohereRerank_NoKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.92},{"index":0,"relevance_score":0.31}]}`))
	}))
	defer server.Close()

	r := NewReranker()
	got, err := r.Rerank(context.Background(), RerankConfig{
		Provider: RerankProviderCohere, BaseURL: server.URL, Model: "bge-reranker-v2-m3",
	}, "query", []string{"doc-a", "doc-b"})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	want := []RerankResult{{Index: 1, Score: 0.92}, {Index: 0, Score: 0.31}}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Index != want[i].Index || math.Abs(got[i].Score-want[i].Score) > 1e-6 {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestReranker_CohereRerank 验证 Cohere /v1/rerank 响应解析（与 Ollama 同构）。
func TestReranker_CohereRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer cohere-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":2,"relevance_score":0.88},{"index":0,"relevance_score":0.55},{"index":1,"relevance_score":0.12}]}`))
	}))
	defer server.Close()

	r := NewReranker()
	got, err := r.Rerank(context.Background(), RerankConfig{
		Provider: RerankProviderCohere, BaseURL: server.URL, Model: "rerank-v3", APIKey: "cohere-key",
	}, "query", []string{"x", "y", "z"})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	want := []RerankResult{{Index: 2, Score: 0.88}, {Index: 0, Score: 0.55}, {Index: 1, Score: 0.12}}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Index != want[i].Index || math.Abs(got[i].Score-want[i].Score) > 1e-6 {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestReranker_NoopAndEmptyDegrade 未配置（Noop / 空 provider）必须返回错误以触发降级。
func TestReranker_NoopAndEmptyDegrade(t *testing.T) {
	if _, err := (NoopReranker{}).Rerank(context.Background(), RerankConfig{}, "q", []string{"a"}); err == nil {
		t.Error("NoopReranker should error")
	}
	if _, err := NewReranker().Rerank(context.Background(), RerankConfig{}, "q", []string{"a"}); err == nil {
		t.Error("empty provider should error")
	}
	if _, err := NewReranker().Rerank(context.Background(), RerankConfig{Provider: "weird"}, "q", []string{"a"}); err == nil {
		t.Error("unsupported provider should error")
	}
}

// TestQnAService_RetrieveChunksHybrid_RerankReorders 验证重排确实改变了召回顺序：
// 用 reverse 的 fakeReranker 时，输出首块应为 RRF 顺序的末块。
func TestQnAService_RetrieveChunksHybrid_RerankReorders(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())
	// 用确定性 tokenEmbedder 拉起向量 leg，让 retrieveChunksHybrid 走完融合路径到达 rerank 分支
	svc := NewQnAServiceWithRegistry(fs, nil, &tokenEmbedder{dim: 64}, &fakeReranker{})
	embCfg := EmbeddingConfig{Model: "test-embed"}

	q := "Go 并发 goroutine"
	rrfOrder := svc.retrieveChunksHybrid(ws, q, embCfg, RerankConfig{}) // rerank 关：纯 RRF 截断
	if len(rrfOrder) < 2 {
		t.Fatalf("need >=2 candidates, got %d", len(rrfOrder))
	}

	svc.reranker = &fakeReranker{reverse: true}
	reranked := svc.retrieveChunksHybrid(ws, q, embCfg, RerankConfig{Provider: RerankProviderCohere, Model: "x"})
	if len(reranked) != len(rrfOrder) {
		t.Fatalf("length changed: %d vs %d", len(reranked), len(rrfOrder))
	}
	// reverse 重排下，首块应为 RRF 末块（候选池未超过 qnaRerankPool，二者同源）
	if reranked[0].chunk.docRelPath != rrfOrder[len(rrfOrder)-1].chunk.docRelPath ||
		reranked[0].chunk.index != rrfOrder[len(rrfOrder)-1].chunk.index {
		t.Errorf("reverse rerank首块应为 RRF 末块: got %s#%d, want %s#%d",
			reranked[0].chunk.docRelPath, reranked[0].chunk.index,
			rrfOrder[len(rrfOrder)-1].chunk.docRelPath, rrfOrder[len(rrfOrder)-1].chunk.index)
	}
	// 顺序确实被重排改变
	if reranked[0].chunk.docRelPath == rrfOrder[0].chunk.docRelPath &&
		reranked[0].chunk.index == rrfOrder[0].chunk.index {
		t.Error("rerank 未改变召回顺序")
	}
}

// TestQnAService_RetrieveChunksHybrid_RerankDegradesToRRF 重排服务出错时，
// 输出必须与「rerank 关」的纯 RRF 结果逐块一致（功能不残废、降级铁律）。
func TestQnAService_RetrieveChunksHybrid_RerankDegradesToRRF(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	fs := NewFileServiceWithHistory(NewSnapshotService())
	embCfg := EmbeddingConfig{Model: "test-embed"}
	q := "Go 并发 goroutine"

	svcRRF := NewQnAServiceWithRegistry(fs, nil, &tokenEmbedder{dim: 64}, &NoopReranker{})
	rffOnly := svcRRF.retrieveChunksHybrid(ws, q, embCfg, RerankConfig{})

	svcErr := NewQnAServiceWithRegistry(fs, nil, &tokenEmbedder{dim: 64}, &fakeReranker{err: true})
	reranked := svcErr.retrieveChunksHybrid(ws, q, embCfg, RerankConfig{Provider: RerankProviderCohere, Model: "x"})

	if len(rffOnly) != len(reranked) {
		t.Fatalf("length mismatch: %d vs %d", len(rffOnly), len(reranked))
	}
	for i := range rffOnly {
		if rffOnly[i].chunk.docRelPath != reranked[i].chunk.docRelPath ||
			rffOnly[i].chunk.index != reranked[i].chunk.index {
			t.Errorf("chunk %d differs: %s#%d vs %s#%d",
				i, rffOnly[i].chunk.docRelPath, rffOnly[i].chunk.index,
				reranked[i].chunk.docRelPath, reranked[i].chunk.index)
		}
	}
}

// TestQnAService_HybridSearch_RerankDegradesToRRF 文档级：rerank 不可用（Noop）时，
// 开启 rerank 配置的输出与关闭时完全一致。
func TestQnAService_HybridSearch_RerankDegradesToRRF(t *testing.T) {
	ClearAllSearchIndexes()
	ws := newQnATestWorkspace(t)
	svc := NewQnAService() // reranker = NoopReranker

	off, errOff := svc.HybridSearch(ws, "Go 并发编程 goroutine", "", "", "", RerankConfig{})
	if errOff != nil {
		t.Fatalf("HybridSearch (rerank off) failed: %v", errOff)
	}
	on, errOn := svc.HybridSearch(ws, "Go 并发编程 goroutine", "", "", "", RerankConfig{Provider: RerankProviderCohere, Model: "x"})
	if errOn != nil {
		t.Fatalf("HybridSearch (rerank on) failed: %v", errOn)
	}
	if len(off) != len(on) {
		t.Fatalf("length mismatch: %d vs %d", len(off), len(on))
	}
	for i := range off {
		if off[i].Path != on[i].Path {
			t.Errorf("doc %d differs: %s vs %s", i, off[i].Path, on[i].Path)
		}
	}
}
