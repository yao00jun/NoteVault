# Workspace Refinement and Main Integration Plan

> **For agentic workers:** Use the dispatching-parallel-agents workflow for the independent template/scaffold and Vault pagination tasks. The primary agent owns file operations, diary cleanup, final verification, and main integration. Steps use checkbox syntax for tracking.

**Goal:** Make file operations work, align templates and workspace initialization with the project workflow, paginate the Vault, remove the user's obsolete diaries without losing their task, and deliver everything on main with one repository directory.

**Architecture:** Keep all user content as Markdown. Reuse TemplateService, the existing workspace scaffold, FileService, the global prompt, editor draft guards, and route state. Upgrade only untouched application defaults; preserve custom templates and reports. No new worktrees or dependencies.

**Tech Stack:** Go (`CGO_ENABLED=0`), Wails v3, Vue 3, Pinia, Vitest, native WebView2 acceptance.

**Spec:** User's 2026-09-09 follow-up: replan Templates and workspace initialization, fix new-folder/rename, remove legacy diaries, paginate Knowledge Vault, test and merge back to main, clean the two extra worktrees.

## Constraints

- Work in the existing `codex/workbench-v3` worktree; do not add another repository directory.
- Markdown remains authoritative; preserve Web Clipper, PARA five spaces, reports, source import, distillation, orb/Copilot, and conflict protections.
- No automatic deletion of arbitrary user diaries on startup. Actual cleanup is confined to the three explicitly identified legacy files in the user's selected demo workspace, with recoverable copies and task preservation.
- Do not overwrite custom starter guides/templates. Never move a native build or hidden internal directory into Learning.
- Workspace-relative paths must not acquire duplicated ancestors; rename must not recreate an old path through delayed saves.
- Merge/push to the existing `origin/main`, verify, then remove only the two explicitly named extra worktree directories. Preserve unique ignored artifacts before cleanup.

## Task 1: Templates and workspace scaffold

Files: `internal/service/templates/*.md`, `templateservice.go`, `templateservice_test.go`, `scaffold.go`, `scaffold_test.go`, optional focused upgrade helper/tests; `frontend/src/components/knowledge/TemplateCreateDialog.vue` and tests if necessary.

- [x] Replace diary-centered defaults with practical Markdown templates for project overview/tasks, technical design/troubleshooting/notes, interview cards, meetings, and work reports.
- [x] Use bundled Markdown as the single definition for new workspace templates. Add descriptions/categories or recommended destinations only if needed for usable selection; preserve arbitrary custom template support.
- [x] Initialize `Learning/面试宝典`, `Daily/Reports` and the existing PARA/support roots. Update starter instructions to Today → Projects → Learning → Reports; avoid fake projects/cards and redundant sample documents.
- [x] Upgrade exact known application defaults, preserving edited variants and all existing work. Retire untouched old diary templates instead of recreating them.
- [x] Cover new/old workspaces, repeat opens, custom templates, rendering and unsafe names with meaningful tests.

## Task 2: Vault pagination

Files: `frontend/src/views/KnowledgeVaultView.vue`, its existing workbench view tests or new focused tests, a small pagination utility/composable if needed.

- [x] Replace growing `visibleLimit` with bounded pages; default 10, options 10/20/50, previous/next and current/total/range indicators.
- [x] Persist valid page/page-size in route query; retain on document round trip. Filter/sort/workspace changes reset appropriately; deletion clamps to a valid page without transient empty states.
- [x] Rename the Daily space label to work logs/reports and remove obsolete diary-oriented visible copy.
- [x] Test 24+ documents, page-size changes, filtering, count shrink, navigation restoration and invalid query values.

## Task 3: File tree operations

Files: `frontend/src/components/editor/FileTree.vue` and tests; `frontend/src/views/EditorView.vue`; focused file-operation composable/tests; existing FileService code/tests if defects surface.

- [x] Reproduce missing new-folder/rename listeners and duplicated recursive paths with failing tests.
- [x] Wire both commands through the global themed prompt. Capture the workspace and node before awaiting input; show actionable errors rather than silent console failures.
- [x] Create in the selected parent, refresh/expand tree. Rename files and folders with preserved extensions and collision/path validation.
- [x] Flush affected dirty tabs and wait for pending writes before rename; reconcile open tab paths, route, pins and recents. Preserve failed or conflicted drafts and guard workspace changes.
- [x] Exercise keyboard/cancel, nested directories, open dirty files and folders, collision and case-only rename where supported.

## Task 4: Remove legacy diary workflow and clean current workspace

- [x] Redirect/remove remaining diary creation entry points and use work-log/report language throughout the touched workflow.
- [x] In `C:/Users/Public/Documents/NoteVault-Demo`, retain both reports; migrate the one unchecked item from `Daily/2026-09-06.md` into `Projects/通用事务/Tasks.md` with its original date.
- [x] Move the three identified old diary Markdown files into the app's recoverable trash after verifying no task/progress is lost. Keep the existing report files byte-for-byte.
- [x] Apply the tested default scaffold/template upgrade to this workspace without changing Java/SQL or user notes.

## Task 5: Verification, build, integration and cleanup

- [ ] Regenerate Wails bindings; run `go test ./internal/...`, `pnpm typecheck`, `pnpm test`, `pnpm lint`, and production build. Repair all failures.
- [ ] Native acceptance: nested create/rename, dirty rename, reports, templates, pagination/filter/back, distillation and three themes; test new/old workspace initialization.
- [ ] Independent review and fix any actionable findings.
- [ ] Commit the result, integrate to `main` in `E:/WorkSpace/NoteVault`, run required verification there, build the distributable and push `origin/main`.
- [ ] Inventory ignored/untracked files in both extra worktrees; preserve useful unique artifacts and remove only `E:/WorkSpace/NoteVault-workbench-experience` and `E:/WorkSpace/NoteVault-workbench-v3` after validating exact resolved paths.
- [ ] Report final main commit, package paths, cleanup result and exact test counts.

## Findings

- Existing v3 worktree has only an EOL-related `go.mod` working-tree status; `git diff -- go.mod` is empty. Preserve contents and verify before integration.
- FileTree emits `new-folder` and `rename` but EditorView does not listen for them. Recursive forwarding also prepends the ancestor to an already workspace-relative parent path.
- Old scaffold and the bundled `Daily` template still create diary-oriented defaults. The demo workspace contains three legacy diaries and two reports; only the September 6 diary has a real unchecked task.
- Vault initially renders up to 40 documents and only offers append-more, explaining the long list.
