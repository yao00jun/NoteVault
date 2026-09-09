package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyScaffoldFixtures(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile("testdata/scaffold-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string][]string
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func writeScaffoldFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScaffoldSeedsWorkflowWithoutInventingWork(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := EnsureWorkspaceScaffold(ws); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range []string{"Learning/面试宝典", "Daily/Reports", "Projects", "Resources", "Inbox", "Templates", "assets"} {
		info, err := os.Stat(filepath.Join(ws, relative))
		if err != nil || !info.IsDir() {
			t.Errorf("missing useful directory %s: %v", relative, err)
		}
	}
	svc := NewTemplateService(NewFileService())
	templates, err := svc.ListTemplates(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range templates {
		if !tpl.Builtin {
			t.Errorf("unmodified seeded template must remain identifiable as bundled: %s", tpl.Name)
		}
		seeded, err := os.ReadFile(filepath.Join(ws, "Templates", tpl.Name+".md"))
		if err != nil {
			t.Errorf("bundled template not seeded: %s: %v", tpl.Name, err)
			continue
		}
		bundled, ok := bundledTemplateContent(tpl.Name)
		if !ok || string(seeded) != bundled {
			t.Errorf("seeded %s must use bundled Markdown as its single definition", tpl.Name)
		}
	}
	for _, relative := range []string{"Templates/Daily.md", "Templates/日记模板.md", "Templates/每周回顾.md", "Templates/待办清单.md", "Templates/项目.md"} {
		if _, err := os.Stat(filepath.Join(ws, relative)); !os.IsNotExist(err) {
			t.Errorf("retired template recreated: %s", relative)
		}
	}
	snapshot, err := NewTodoService().GetWorkbench(ws, "2026-09-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks)+len(snapshot.Projects)+len(snapshot.Books)+len(snapshot.Cards) != 0 {
		t.Fatalf("starter content must not fabricate work: %+v", snapshot)
	}
}

func TestScaffoldUpgradesOnlyKnownDefaultsAndKeepsBackups(t *testing.T) {
	retired := map[string]bool{"Templates/Daily.md": true, "Templates/日记模板.md": true, "Templates/每周回顾.md": true, "Templates/待办清单.md": true, "Templates/项目.md": true}
	for relative, variants := range legacyScaffoldFixtures(t) {
		for _, old := range variants {
			for _, ending := range []string{"LF", "CRLF"} {
				t.Run(relative+ending, func(t *testing.T) {
					ws := t.TempDir()
					before := old
					if ending == "CRLF" {
						before = strings.ReplaceAll(strings.ReplaceAll(old, "\r\n", "\n"), "\n", "\r\n")
					}
					writeScaffoldFixture(t, ws, relative, before)
					for i := 0; i < 2; i++ {
						if err := EnsureWorkspaceScaffold(ws); err != nil {
							t.Fatal(err)
						}
					}
					after, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(relative)))
					if retired[relative] {
						if !os.IsNotExist(err) {
							t.Fatalf("unchanged retired template should leave chooser: %s", relative)
						}
					} else if err != nil || string(after) == before {
						t.Fatalf("unchanged old default was not upgraded: %s, %v", relative, err)
					}
					backup, err := os.ReadFile(filepath.Join(ws, ".notevault/scaffold-backups/2026-09-09", filepath.FromSlash(relative)))
					if err != nil || string(backup) != before {
						t.Fatalf("original default must be recoverable byte for byte: %s, %v", relative, err)
					}
				})
			}
		}
	}
}

func TestScaffoldPreservesEditedDefaultsAndAllUserLogs(t *testing.T) {
	ws := t.TempDir()
	contents := map[string]string{
		"Daily/2026-09-06.md": "# 日记\r\n- [ ] 我的任务\r\n",
		"Daily/Reports/日报.md": "# 不能修改的日报\n",
		"Daily/Reports/周报.md": "# 不能修改的周报\n",
		"Templates/团队模板.md":   "# {{title}}\n{{custom}}\n",
	}
	for relative, variants := range legacyScaffoldFixtures(t) {
		contents[relative] = variants[0] + "\n我调整过这份内容。\n"
	}
	for relative, content := range contents {
		writeScaffoldFixture(t, ws, relative, content)
	}
	for i := 0; i < 2; i++ {
		if err := EnsureWorkspaceScaffold(ws); err != nil {
			t.Fatal(err)
		}
	}
	for relative, before := range contents {
		after, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(relative)))
		if err != nil || string(after) != before {
			t.Errorf("user content changed: %s, %v", relative, err)
		}
	}
}

func TestScaffoldKeepsArbitraryRootFoldersAndBuildsInPlace(t *testing.T) {
	ws := t.TempDir()
	for _, relative := range []string{"新文件夹/笔记.md", "build/README.md", "bin/NoteVault.exe", ".notevault/state", ".git/config", "Java/基础.md", "SQL/查询.md"} {
		writeScaffoldFixture(t, ws, relative, relative)
	}
	if err := EnsureWorkspaceScaffold(ws); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"新文件夹/笔记.md", "build/README.md", "bin/NoteVault.exe", ".notevault/state", ".git/config", "Learning/Java/基础.md", "Learning/SQL/查询.md"} {
		if _, err := os.Stat(filepath.Join(ws, relative)); err != nil {
			t.Errorf("folder incorrectly migrated or historical migration missing: %s, %v", relative, err)
		}
	}
}

func TestScaffoldRejectsLinkedDestinations(t *testing.T) {
	for _, relative := range []string{"Templates", "Daily", "Learning", "开始使用.md"} {
		t.Run(relative, func(t *testing.T) {
			ws, outside := t.TempDir(), t.TempDir()
			target := outside
			if strings.HasSuffix(relative, ".md") {
				target = filepath.Join(outside, "original.md")
				if err := os.WriteFile(target, []byte("# outside"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(ws, relative)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := EnsureWorkspaceScaffold(ws); err == nil {
				t.Fatal("linked scaffold path must be rejected")
			}
			after, err := os.ReadDir(outside)
			if err != nil || len(after) != len(before) {
				t.Fatalf("scaffold changed outside directory: %v", err)
			}
		})
	}
}
