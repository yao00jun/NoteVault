package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoadmapService_ParsesTracksHoursAndProgress 用与 AI转型路径-13周.md 同构的
// 夹具验证：分轨统计、估时汇总、完成度百分比、非轨分节跳过。
func TestRoadmapService_ParsesTracksHoursAndProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Learning", "Java"), 0750); err != nil {
		t.Fatal(err)
	}
	doc := "---\nkind: roadmap\ngoal: 三个月内能承担 AI 应用开发任务\ndeadline: 2027-02-22\nweeklyHours: 6\n---\n# AI 转型路径（13 周）\n\n> 路径是唯一数据源，这里只做投影。\n\n## 总预算\n\n- 13 周 × 6h ≈ 78h\n\n## 轨一：语言基础\n\n- [x] [[第1章 环境与工具]] #est/1h\n- [ ] [[第2章 语法基础]] #est/1.5h\n- [ ] [[Java基础]] #est/2.5h\n\n## 轨二：AI 应用开发\n\n- [ ] [[AI应用开发#第1章 LLM 与 API 调用]] #est/2.5h\n\n## 本期不做\n\n- 微调 / 训练相关（无勾选、无估时）\n"
	if err := os.WriteFile(filepath.Join(root, "Learning", "AI转型路径-13周.md"), []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}
	// 非 roadmap 的书架 book.md 与子文件夹内容都不进投影
	if err := os.WriteFile(filepath.Join(root, "Learning", "Java", "book.md"), []byte("---\nname: Java\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Learning", "非路径笔记.md"), []byte("# 随手记\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewRoadmapService()
	roadmaps, err := svc.GetRoadmapProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(roadmaps) != 1 {
		t.Fatalf("only one roadmap should be projected, got %d: %+v", len(roadmaps), roadmaps)
	}
	rm := roadmaps[0]
	if rm.Path != "Learning/AI转型路径-13周.md" || rm.Goal == "" || rm.Deadline != "2027-02-22" || rm.WeeklyHours != "6" {
		t.Fatalf("unexpected frontmatter projection: %+v", rm)
	}
	// 轨：总预算（无勾选）与本期不做（无勾选）都不成轨
	if len(rm.Tracks) != 2 {
		t.Fatalf("expected 2 tracks with checkboxes, got %d: %+v", len(rm.Tracks), rm.Tracks)
	}
	if rm.Tracks[0].Name != "轨一：语言基础" || rm.Tracks[0].Total != 3 || rm.Tracks[0].Done != 1 || rm.Tracks[0].Percent != 33 {
		t.Fatalf("unexpected track 1: %+v", rm.Tracks[0])
	}
	// 估时：完成 1h / 全轨 5h（1 + 1.5 + 2.5）
	if rm.Tracks[0].DoneHours != 1 || rm.Tracks[0].TotalHours != 5 {
		t.Fatalf("unexpected hours: %+v", rm.Tracks[0])
	}
	if rm.NextStep != "[[第2章 语法基础]] #est/1.5h" || rm.NextHours != 1.5 {
		t.Fatalf("unexpected next step: %q (%vh)", rm.NextStep, rm.NextHours)
	}
	if rm.Tracks[1].Next != "[[AI应用开发#第1章 LLM 与 API 调用]] #est/2.5h" {
		t.Fatalf("unexpected track 2 next: %q", rm.Tracks[1].Next)
	}
	if rm.Total != 4 || rm.Done != 1 || rm.Percent != 25 {
		t.Fatalf("unexpected overall: %+v", rm)
	}
	if rm.TotalHours != 7.5 || rm.DoneHours != 1 {
		t.Fatalf("unexpected overall hours: %+v", rm)
	}
}

func TestRoadmapService_EmptyLearningFolderAndMissingDir(t *testing.T) {
	svc := NewRoadmapService()
	root := t.TempDir()
	roadmaps, err := svc.GetRoadmapProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(roadmaps) != 0 {
		t.Fatalf("empty learning folder should project nothing: %+v", roadmaps)
	}
	if roadmaps == nil {
		t.Fatal("empty result must be an empty slice, not nil")
	}
	roadmaps, err = svc.GetRoadmapProgress(filepath.Join(root, "不存在"))
	if err != nil {
		t.Fatal(err)
	}
	if len(roadmaps) != 0 {
		t.Fatalf("missing Learning dir should project nothing: %+v", roadmaps)
	}
}

// TestRoadmapService_NoFrontmatterFileSkipped 无 frontmatter 或 kind 不是 roadmap 时跳过。
func TestRoadmapService_NoFrontmatterFileSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Learning"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Learning", "无标记.md"), []byte("## 轨一\n\n- [ ] 任务\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Learning", "其他类型.md"), []byte("---\nkind: book\n---\n## 轨一\n\n- [ ] 任务\n"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := NewRoadmapService()
	roadmaps, err := svc.GetRoadmapProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(roadmaps) != 0 {
		t.Fatalf("non-roadmap files must be skipped: %+v", roadmaps)
	}
	_ = strings.TrimSpace("")
}
