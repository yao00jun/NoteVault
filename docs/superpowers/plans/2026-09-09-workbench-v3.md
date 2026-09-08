# Workbench v3 Implementation Plan

> **For agentic workers:** Use the dispatching-parallel-agents workflow for the independent backend and task-flow domains; the primary agent owns editor integration and final verification. Steps use checkbox syntax for tracking.

**Goal:** Replace diary-oriented entry points with the daily work report, keep new tasks in projects, and distill project experience into linked book chapters or interview cards.

**Architecture:** Keep TodoService as the Markdown domain boundary and Pinia as a disposable projection. Reuse the existing report generator, safe Markdown change writer, SRS parser, AI configuration, and editor draft/conflict lifecycle. The source note must be saved before distillation and reconciled after the backend appends its backlink.

**Tech Stack:** Go with `CGO_ENABLED=0`, Wails v3, Vue 3, Pinia, TypeScript, Vitest.

**Spec:** `docs/design/WORKBENCH-V3-ENGINEERING-SPEC.md` (finalized and authorized by the user).

## Global constraints

- Markdown is the single source of truth; no new database or native dependency.
- Preserve all existing suites, Web Clipper, PARA scaffold, navigation, AI floating orb and Copilot.
- Existing daily notes remain readable. New task/progress actions must not create a second daily diary.
- Match the established three themes through existing tokens and controls.
- Never replace existing target chapters, source drafts, SRS answers, or unrelated frontmatter.

## Task 1: Project-owned work flow

**Files:** `internal/service/workbench_mutations.go`, `internal/service/workbench_mutation_test.go`, `frontend/src/components/layout/SideBar.vue`, `frontend/src/views/TodayView.vue`, their tests; new `frontend/src/composables/useWorkLog.ts` and tests; a focused default-project utility if necessary.

**Interfaces:** Keep `AddWorkbenchTask(workspacePath, projectFolder, title, kind, due, date) error`. An empty folder resolves to `Projects/通用事务`; explicitly selecting this group also initializes `project.md` if absent. Existing selected projects must still exist. `useWorkLog()` provides `openTodayWorkLog(): Promise<boolean>` using `ReadDailyReport` and the `notevault:daily-report` event.

- [x] Add and run regressions for existing/missing report navigation, no diary creation, active/frequent project selection, general-project creation and project-only progress.
- [x] Read today's saved report before routing to `Daily/Reports/${day}-日报.md`; when absent, notify and invoke the existing generator. Preserve workspace/session guards.
- [x] Default the new task selector to the active project with recent task activity (then recent modification), with `通用事务` as the fallback. Show the resulting `Projects/<name>/Tasks.md` destination.
- [x] Keep progress below the original Markdown task and derive the timeline/report from that single source. Preserve completion metadata and legacy task editing.
- [x] Run targeted Go mutation and frontend sidebar/Today/work-log tests.

## Task 2: Markdown knowledge distillation backend

**Files:** new `internal/service/workbench_distill.go` and `workbench_distill_test.go`; `internal/service/ports.go`.

**Interface:**

```go
type DistillRequest struct {
    SourceFile string `json:"sourceFile"`
    TargetMode string `json:"targetMode"`
    TargetFolder string `json:"targetFolder"`
    TargetTitle string `json:"targetTitle"`
    Question string `json:"question"`
    Answer string `json:"answer"`
    Summary string `json:"summary"`
}
func (s *TodoService) DistillKnowledge(workspacePath string, req DistillRequest) error
```

- [x] Add and run tests for book metadata/backlinks, new book discovery, interview append/SRS review, duplicate requests, title collisions, unsafe paths and source preservation.
- [x] Validate workspace-relative Markdown sources under `Projects/<project>/` and book folders under `Learning/<book>` or the fixed interview directory. Use `confineToWorkspace` plus existing symlink/alternate-stream protection.
- [x] Create `book.md` only when absent; chapter frontmatter contains `type`, `sourceProject`, `distilledAt` and source-derived tags. Do not overwrite an existing chapter. Use a deterministic request marker for retry detection while leaving normal Markdown usable.
- [x] Append uniquely identified interview questions, initial `掌握`/7-day/reps-1 SRS JSON and source reference. Preserve the topic file and subsequent SRS review state across retries.
- [x] Commit target metadata/chapter or card plus the source badge through the existing optimistic safe-write mechanism, with honest partial-failure errors.
- [x] Run focused backend distillation tests, including malformed user input and retry recovery.

## Task 3: Editor and distillation modal

**Files:** new `frontend/src/components/workbench/DistillKnowledgeModal.vue` and component tests; `frontend/src/views/EditorView.vue`; `frontend/src/composables/useEditorDraft.ts` and tests only for necessary external-write reconciliation; `frontend/src/api/workbench.ts`; `frontend/src/stores/workbench.ts`; focused distillation utility/composable tests.

**Interfaces:** `WorkbenchService.DistillKnowledge(workspacePath, request): Promise<void>`; store mutation refreshes the workspace tree and workbench. Modal accepts an immutable source context (workspace, path, title, content), submits an editable request, and closes only after persistence succeeds. Editor owns saving/reconciling the source tab.

- [x] Add failing component tests for mode selection, validation, request content, errors, duplicate-click suppression, workspace changes, and AI draft application.
- [x] Show a highlighted `Zap` action only for project Markdown files. Flush the source and reject unresolved conflicts before opening the modal.
- [x] Offer existing/new books, note title and summary, or interview topic/question/answer. Show the source, destination and links the operation will create.
- [x] Reuse the configured AI request path to propose a draft only on explicit click. Show errors without destroying manual text; require deliberate draft application when user edits exist.
- [x] Flush again before backend mutation, prevent autosave from overwriting the appended badge, and reconcile clean or concurrently edited tabs without dropping their text.
- [x] Run focused editor, modal, store and AI tests, preserving all existing draft/conflict scenarios.

## Task 4: Integration and verification

**Files:** regenerated Wails bindings; any directly affected documentation.

- [x] Regenerate bindings with `wails3 generate bindings ./... -clean=true -ts -i -names` under `CGO_ENABLED=0`.
- [x] Review requirements and changes, including source/target safety, SRS discoverability, stale workspace responses, modal keyboard behavior and themed layout.
- [x] Run `go test ./internal/...`, `pnpm typecheck`, `pnpm test`, `pnpm lint` and production build; fix all failures.
- [x] Exercise actual Wails/WebView2 flows in an isolated temporary workspace: report navigation, project tasks and timeline, book/interview distillation, linked note access and saved source badge. Browser plugin is absent; reuse Playwright CDP against Wails and keep artifacts outside the repo.
- [x] Report exact verification outputs and changed files. V2 work is already delivered and must not be repeated.

## Verification log

- Starting point: clean `d7cd23c`, branch `codex/workbench-v3`, worktree `E:/WorkSpace/NoteVault-workbench-v3`.
- Baseline verified: 59 suites / 615 tests passed; 11 pre-existing lint warnings.

## Final verification — 2026-09-09

- `CGO_ENABLED=0 go test ./internal/...`: all 10 packages passed. A fresh `-count=1` run also passed (service: 22.512s); final service run: 21.065s.
- `pnpm typecheck`: exit 0, no errors.
- `pnpm test`: 65 suites / 684 tests passed, 16.24s.
- `pnpm lint`: exit 0, 0 errors / 10 existing warnings. No new lint warnings; one previous toolbar formatting warning was removed.
- `wails3 generate bindings ./... -clean=true -ts -i -names`: exit 0; 28 services / 133 methods.
- `pnpm build` and native production `go build`: exit 0. Binary metadata confirms `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64`.
- Native Wails/WebView2 acceptance: 13 flows passed, including a full restart. Real temporary Markdown files verified; no page exceptions. Three themes inspected at 1440×960 and 1000×720.
- Independent review findings fixed and rechecked: case-alias save reservation, interview heading containment and CRLF fenced examples.
- Native acceptance found a pre-existing basename-only wiki-link resolver that broke the new full-path backlinks. Extracted `wikiLinkFiles.ts`, added five regressions, and verified source/chapter round trips in the native app.
- AI draft proposal, errors, user edits during requests, and deliberate application verified in component tests against the real composable with a mocked provider response; no live remote model was used.
- Full file manifest and behavior evidence: `docs/design/WORKBENCH-V3-ACCEPTANCE.md`.