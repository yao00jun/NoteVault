package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/philippgille/chromem-go"
)

// ---------------------------------------------------------------------------
// VectorStore（P1-3 的 vector leg）
//
// 包装 chromem-go（纯 Go、零 CGO），只承担「向量检索」这一条腿，
// 不接管 BM25——BM25 仍由 SearchService / QnAService 的内存索引负责。
// 这样「未配置 embedding → 不建索引 → 检索退化为纯 BM25」是零成本开关，
// 而非需要特判的分支。
//
// 所有派生数据都在 <workspace>/.notevault/vectors/ 下，删除即回退纯 BM25
// （红线 #2：派生数据不污染笔记目录）。
// 持久化目录名包含模型名哈希：换模型维度变化后自动走新目录重建。
// ---------------------------------------------------------------------------

// vectorHit 是向量检索命中的一块（含溯源信息与块原文，用于融合后注入上下文）。
type vectorHit struct {
	RelPath    string  // 文档相对路径
	Heading    string  // 该块所属章节
	ChunkIndex int     // 块在文档内的序号
	Content    string  // 块原文（chromem 持久化存储，无需回索引取）
	Similarity float32 // 余弦相似度（chromem 返回，范围 [-1,1]）
}

// VectorStore 包装一个 chromem collection（集合名固定为 "notes"）。
type VectorStore struct {
	db   *chromem.DB
	coll *chromem.Collection
}

// hashDirName 计算一个稳定的短哈希，用于派生目录名。
func hashDirName(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// persistDirForWorkspace 返回工作区 + 模型对应的向量索引目录。
// 模型名进入目录名，换模型后维度不匹配问题通过「换目录」自然解决。
func persistDirForWorkspace(workspacePath, model string) string {
	return filepath.Join(workspacePath, ".notevault", "vectors",
		hashDirName(workspacePath)+"_"+hashDirName(model))
}

// newChromemEmbedFunc 把我们的批量 Embedder 适配成 chromem 需要的单文本 EmbeddingFunc。
// chromem 在 Query 时会用它嵌入查询文本（每次一个）；构建索引时我们用预计算
// embeddings 路径，不会走这里。
func newChromemEmbedFunc(embedder Embedder, cfg EmbeddingConfig) chromem.EmbeddingFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		vecs, err := embedder.Embed(ctx, cfg, []string{text})
		if err != nil {
			return nil, err
		}
		if len(vecs) == 0 {
			return nil, fmt.Errorf("embedding 返回为空")
		}
		return vecs[0], nil
	}
}

// NewVectorStore 打开（或创建）持久化于 persistDir 的向量索引。
//
// chromem 持久化加载后，集合的 embed 函数不可序列化，必须在 GetCollection 时
// 重新传入。若集合尚不存在（首次），则创建空集合，待 Build 填充。
func NewVectorStore(persistDir string, embedder Embedder, cfg EmbeddingConfig) (*VectorStore, error) {
	if err := os.MkdirAll(persistDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建向量索引目录失败：%w", err)
	}
	db, err := chromem.NewPersistentDB(persistDir, false)
	if err != nil {
		return nil, fmt.Errorf("打开向量索引失败：%w", err)
	}
	chromemEmbed := newChromemEmbedFunc(embedder, cfg)
	coll := db.GetCollection("notes", chromemEmbed)
	if coll == nil {
		coll, err = db.CreateCollection("notes", nil, chromemEmbed)
		if err != nil {
			return nil, fmt.Errorf("创建向量集合失败：%w", err)
		}
	}
	return &VectorStore{db: db, coll: coll}, nil
}

// Count 返回已索引的向量块数（0 表示尚未构建）。
func (vs *VectorStore) Count() int {
	return vs.coll.Count()
}

// Build 遍历工作区所有 Markdown，分块后批量嵌入并写入集合。
// 已构建（Count > 0）则跳过。失败返回错误，由调用方决定是否回退。
func (vs *VectorStore) Build(ctx context.Context, workspacePath string, fileService *FileService, embedder Embedder, cfg EmbeddingConfig) error {
	if vs.coll.Count() > 0 {
		return nil
	}

	files, err := collectMarkdownFiles(workspacePath)
	if err != nil {
		return err
	}

	type pending struct {
		id   string
		meta map[string]string
		text string
	}
	var pendings []pending
	for _, f := range files {
		content, err := fileService.ReadFile(workspacePath, f.rel)
		if err != nil {
			// 单文件读取失败不阻断整体索引，跳过即可
			continue
		}
		chunks := chunkDocument(f.rel, f.base, content, qnaChunkMaxRunes, qnaChunkOverlapRunes)
		for _, ch := range chunks {
			pendings = append(pendings, pending{
				id: fmt.Sprintf("%s#%d", ch.docRelPath, ch.index),
				meta: map[string]string{
					"relPath":    ch.docRelPath,
					"heading":    ch.heading,
					"chunkIndex": strconv.Itoa(ch.index),
				},
				text: ch.text,
			})
		}
	}
	if len(pendings) == 0 {
		return nil
	}

	// 批量嵌入并写入，分批控制单请求体积（Ollama 单次 input 过大可能超时）。
	const batchSize = 64
	for i := 0; i < len(pendings); i += batchSize {
		end := i + batchSize
		if end > len(pendings) {
			end = len(pendings)
		}
		batch := pendings[i:end]
		texts := make([]string, len(batch))
		for j, p := range batch {
			texts[j] = p.text
		}
		embeddings, err := embedder.Embed(ctx, cfg, texts)
		if err != nil {
			return fmt.Errorf("生成向量失败：%w", err)
		}
		if len(embeddings) != len(batch) {
			return fmt.Errorf("embedding 返回数量与输入不符（期望 %d，实际 %d）", len(batch), len(embeddings))
		}
		ids := make([]string, len(batch))
		metas := make([]map[string]string, len(batch))
		contents := make([]string, len(batch))
		for j, p := range batch {
			ids[j] = p.id
			metas[j] = p.meta
			contents[j] = p.text
		}
		if err := vs.coll.Add(ctx, ids, embeddings, metas, contents); err != nil {
			return fmt.Errorf("写入向量失败：%w", err)
		}
	}
	return nil
}

// Search 返回与 query 最相似的 Top-K 块（vector leg 原始结果）。
// 结果已是按相似度降序，调用方再做 RRF 融合或直接使用。
func (vs *VectorStore) Search(ctx context.Context, query string, k int) ([]vectorHit, error) {
	if k <= 0 {
		k = 20
	}
	// chromem 要求 nResults 不能超过集合中的文档数，超出会直接报错。
	// 这里 clamp 到实际规模，避免小知识库（块数 < k）查询时崩溃。
	n := vs.coll.Count()
	if n == 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	results, err := vs.coll.Query(ctx, query, k, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("向量检索失败：%w", err)
	}
	hits := make([]vectorHit, 0, len(results))
	for _, r := range results {
		ci, _ := strconv.Atoi(r.Metadata["chunkIndex"])
		hits = append(hits, vectorHit{
			RelPath:    r.Metadata["relPath"],
			Heading:    r.Metadata["heading"],
			ChunkIndex: ci,
			Content:    r.Content,
			Similarity: r.Similarity,
		})
	}
	return hits, nil
}
