package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errFakeAI = errors.New("fake ai failure")

// fakeCompileAI 是 CompileAI 的桩实现，不联网，返回可预测结果。
type fakeCompileAI struct {
	out *CompileOutput
	err error
}

func (f *fakeCompileAI) Compile(_, _, _, _, _ string) (*CompileOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

// seedVault 造一个含 Inbox 的工作区，返回工作区路径。
func seedCompileVault(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	inbox := filepath.Join(ws, "Inbox")
	if err := os.MkdirAll(inbox, 0750); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: 缓存设计\ntags: [system]\n---\n# 缓存设计\n\n聊聊缓存失效策略与 TTL 设置。\n"
	if err := os.WriteFile(filepath.Join(inbox, "cache.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// 无 frontmatter 的笔记
	if err := os.WriteFile(filepath.Join(inbox, "plain.md"), []byte("# 纯文本笔记\n\n一些内容。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Inbox 子目录里的笔记
	sub := filepath.Join(inbox, "drafts")
	_ = os.MkdirAll(sub, 0750)
	if err := os.WriteFile(filepath.Join(sub, "deep.md"), []byte("# 深层草稿\n\n正文。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return ws
}

func newTestCompileService(ai CompileAI) *CompileService {
	return NewCompileService(NewFileService(), NewSnapshotService(), ai, "Inbox", "Compiled")
}

func TestCompileService_ListInbox(t *testing.T) {
	ws := seedCompileVault(t)
	svc := newTestCompileService(&fakeCompileAI{out: &CompileOutput{}})

	notes, err := svc.ListInbox(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("期望 3 篇 Inbox 笔记，实际 %d: %v", len(notes), notes)
	}
	// 子目录里也应被递归列出
	foundDeep := false
	for _, n := range notes {
		if strings.Contains(n, "drafts/deep.md") {
			foundDeep = true
		}
	}
	if !foundDeep {
		t.Errorf("未递归列出子目录笔记: %v", notes)
	}
}

func TestCompileService_CompileNote_MovesAndKeepsFrontmatter(t *testing.T) {
	ws := seedCompileVault(t)
	svc := newTestCompileService(&fakeCompileAI{out: &CompileOutput{
		TLDR:           "缓存失效与 TTL 实践",
		Tags:           []string{"system", "cache", "performance"},
		SuggestedLinks: []string{"Redis 入门", "分布式缓存"},
	}})

	res, err := svc.CompileNote(ws, "Inbox/cache.md", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Dest != "Compiled/cache.md" {
		t.Fatalf("目标路径错误: %s", res.Dest)
	}
	if res.SnapshotID == "" {
		t.Error("应建立撤销快照")
	}

	// Inbox 原文已删除
	if _, err := svc.fileSvc.ReadFile(ws, "Inbox/cache.md"); err == nil {
		t.Error("Inbox 原文应已被移走")
	}
	// Compiled 新文件存在
	got, err := svc.fileSvc.ReadFile(ws, "Compiled/cache.md")
	if err != nil {
		t.Fatalf("读取编译结果失败: %v", err)
	}
	// 用户原有 frontmatter（title）应保留，且新增 tldr/tags/compiled
	if !strings.Contains(got, "title: 缓存设计") {
		t.Errorf("原 frontmatter 的 title 丢失:\n%s", got)
	}
	if !strings.Contains(got, "tldr: 缓存失效与 TTL 实践") {
		t.Errorf("未写入 tldr:\n%s", got)
	}
	if !strings.Contains(got, "tags: [system, cache, performance]") {
		t.Errorf("未写入合并后的 tags:\n%s", got)
	}
	if !strings.Contains(got, "compiled: true") {
		t.Errorf("未标记 compiled:\n%s", got)
	}
	if !strings.Contains(got, "## 相关笔记（编译建议）") {
		t.Errorf("未追加双链建议小节:\n%s", got)
	}
	if !strings.Contains(got, "[[Redis 入门]]") || !strings.Contains(got, "[[分布式缓存]]") {
		t.Errorf("双链建议未正确呈现:\n%s", got)
	}
	// 正文原内容保留
	if !strings.Contains(got, "聊聊缓存失效策略") {
		t.Errorf("正文原内容丢失:\n%s", got)
	}
}

func TestCompileService_CompileNote_RejectsNonInbox(t *testing.T) {
	ws := seedCompileVault(t)
	svc := newTestCompileService(&fakeCompileAI{out: &CompileOutput{}})

	_, err := svc.CompileNote(ws, "Other/note.md", "", "", "")
	if err == nil {
		t.Fatal("非 Inbox 路径应被拒绝")
	}
}

func TestCompileService_CompileNote_AICreateFailKeepsOriginal(t *testing.T) {
	ws := seedCompileVault(t)
	svc := newTestCompileService(&fakeCompileAI{err: errFakeAI})

	_, err := svc.CompileNote(ws, "Inbox/cache.md", "", "", "")
	if err == nil {
		t.Fatal("AI 失败应报错")
	}
	// 原文必须还在
	if _, err := svc.fileSvc.ReadFile(ws, "Inbox/cache.md"); err != nil {
		t.Errorf("AI 失败时原文不应被改动: %v", err)
	}
	if _, err := svc.fileSvc.ReadFile(ws, "Compiled/cache.md"); err == nil {
		t.Error("AI 失败时不应产生 Compiled 副本")
	}
}

func TestCompileService_CompileAll(t *testing.T) {
	ws := seedCompileVault(t)
	svc := newTestCompileService(&fakeCompileAI{out: &CompileOutput{
		TLDR: "x", Tags: []string{"t"}, SuggestedLinks: []string{"A"},
	}})

	results, errs := svc.CompileAll(ws, "", "", "")
	if len(errs) != 0 {
		t.Fatalf("不应有错误: %v", errs)
	}
	if len(results) != 3 {
		t.Fatalf("期望编译 3 篇，实际 %d", len(results))
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Dest, "Compiled/") {
			t.Errorf("结果未进入 Compiled: %s", r.Dest)
		}
	}
}

func TestParseCompileOutput(t *testing.T) {
	raw := "TLDR: 一句话摘要\nTAGS: a, b, c\nLINKS: [[笔记一]], [[笔记二]]"
	out, err := parseCompileOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.TLDR != "一句话摘要" {
		t.Errorf("TLDR 解析错误: %q", out.TLDR)
	}
	if strings.Join(out.Tags, ",") != "a,b,c" {
		t.Errorf("Tags 解析错误: %v", out.Tags)
	}
	if strings.Join(out.SuggestedLinks, ",") != "笔记一,笔记二" {
		t.Errorf("Links 解析错误: %v", out.SuggestedLinks)
	}
}

func TestMergeCompileMeta_PreservesOtherKeys(t *testing.T) {
	fm := "title: 原标题\nauthor: bob\ntags:\n  - old1\n  - old2\ntldr: 旧摘要\nbody: keep"
	merged := mergeCompileMeta(fm, &CompileOutput{
		TLDR: "新摘要", Tags: []string{"x", "y"},
	}, "2026-08-31T00:00:00Z")

	if !strings.Contains(merged, "title: 原标题") {
		t.Errorf("title 丢失:\n%s", merged)
	}
	if !strings.Contains(merged, "author: bob") {
		t.Errorf("author 丢失:\n%s", merged)
	}
	if !strings.Contains(merged, "body: keep") {
		t.Errorf("body 键应保留:\n%s", merged)
	}
	if !strings.Contains(merged, "tldr: 新摘要") {
		t.Errorf("tldr 未更新:\n%s", merged)
	}
	if !strings.Contains(merged, "tags: [x, y]") {
		t.Errorf("tags 未替换为新值:\n%s", merged)
	}
	if strings.Contains(merged, "old1") || strings.Contains(merged, "旧摘要") {
		t.Errorf("旧 tags 续行 / 旧 tldr 未被清除:\n%s", merged)
	}
	if !strings.Contains(merged, "compiled: true") {
		t.Errorf("缺少 compiled 标记:\n%s", merged)
	}
}
