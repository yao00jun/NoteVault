# Workbench Experience Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Execute continuously; the user already authorized implementation, verification, packaging, commit and push.

**Goal:** Make navigation predictable, collections easy to initialize from real sources, and all three themes coherent without losing existing features.

**Architecture:** A router-aware application navigation store owns context and view history. Existing ImportService gains a bounded Markdown ingestion pipeline over TaskService; a shared frontend modal calls its explicit APIs. Existing components and theme tokens are reused.

**Tech Stack:** Vue 3, Pinia, Vue Router, TypeScript, Vitest, Go 1.25, Wails 3, pure-Go extractors, native WebView2/Playwright QA.

**Spec:** docs/design/WORKBENCH-EXPERIENCE-SPEC.md (extends E:/WorkSpace/NoteVault/docs/design/WORKBENCH-V2-FULL-SPEC.md).

## Global Constraints

- Markdown is the single source of truth; SQLite and in-memory indices are derived caches only.
- Preserve Web Clipper, five spaces/scaffold, editor draft/conflict guards, global AI orb/drawer, templates, exports and legacy routes.
- Pure Go, CGO_ENABLED=0. Do not execute imported code, scripts or source instructions.
- Work in E:/WorkSpace/NoteVault-workbench-experience on codex/workbench-experience. Do not edit the original checkout.
- No implementation agent spawns another agent or commits shared files. Controller coordinates integration/review/commits.
- Start with meaningful failing regressions, implement, and verify the relevant tests. Never weaken a valid regression to conceal failure.

### Task 1: Markdown source ingestion backend

**Owner/files:** backend agent owns internal/service/sourceimport*.go, narrow extensions to importservice.go, ports.go/ports_contract_test.go if needed, and go.mod/go.sum. Do not edit frontend bindings (controller regenerates), App, router, or styles. Existing ImportService is registered already; add exported methods there rather than a new registration.

**Interfaces produced (JSON field names are binding):**

```ts
type CollectionKind = 'project' | 'book' | 'topic'
interface SourceImportFile { name: string; contentBase64: string }
interface SourceImportRequest {
  sourceType: 'empty' | 'folder' | 'adopt' | 'files' | 'url'
  source: string
  files: SourceImportFile[]
  kind: CollectionKind
  name: string
  targetFolder: string
  conflictStrategy: 'skip' | 'update' | 'copy'
  enrich: boolean
  instruction: string
  ai: { apiKey: string; baseURL: string; model: string }
}
interface SourceImportPreview {
  name: string; kind: CollectionKind; targetFolder: string; metadataPath: string
  fileCount: number; files: string[]; warnings: string[]; existing: boolean
}
interface SourceImportResult {
  taskId: string; targetFolder: string; metadataPath: string
  imported: number; skipped: number; updated: number
  conflicts: string[]; warnings: string[]; files: string[]; cancelled: boolean
}
// Wails ImportService methods:
// PreviewSourceImport(workspacePath, request) -> SourceImportPreview (read-only)
// StartSourceImport(workspacePath, request) -> string taskId
// GetSourceImportResult(workspacePath, taskId) -> SourceImportResult
```

All absent strings/arrays use their zero/empty values. Name may be inferred from the source. Empty requires name. Default target is Projects/name, Learning/name or Resources/name. Explicit targetFolder is workspace-relative, confined to the corresponding space. Adopt uses an existing folder in that space without rewriting its notes. Metadata returns a real project.md/book.md/index.md. Use the same project/book field aliases the workbench reader understands; default progress zero, status planned/unread, no fabricated tasks or completions.

- [ ] Write tests that assert Markdown metadata reconstructs in TodoService.GetWorkbench, same source import is idempotent, update preserves a locally changed file, adopt preserves all bytes, and invalid paths fail before writing. Include representative DOCX/EPUB/PDF/HTML text fixtures, cancellation with partial result, credential omission and AI failure fallback. Example contract:
```go
result := runSourceImport(t, svc, ws, request)
requireFileContains(t, ws, result.MetadataPath, "name:")
second := runSourceImport(t, svc, ws, request)
if second.Imported != 0 { t.Fatal("duplicate content must be skipped") }
```
- [ ] Run the targeted new tests and observe missing API failures.
- [ ] Implement helpers split by extraction, safe write/manifest and task orchestration. Use existing TaskService cancellation and result ownership by workspace. Bound files to 20 MiB each, aggregate 100 MiB and 500 files; bound archives and HTTP, reject path escapes/symlinks and private network URLs/redirects. Ignore .git, node_modules, generated/cache and credential files. Preserve useful relative structure/assets. Public GitHub repository URLs can use a bounded archive; bare webpage imports one page. Unsupported/OCR-required sources produce clear warnings, never fake extracted text.
- [ ] Store import hashes and origins in Markdown (e.g. sources.md with a structured HTML comment); preserve existing metadata and user edits. Persist per-file progress early enough that cancelled/retried imports stay idempotent. Return partial results on cancellation/failure. AI enrichment uses existing LLM infrastructure with untrusted-source framing, bounded input and separate clearly labeled Markdown output. API keys stay only in memory. Do not trust AI-provided paths.
- [ ] Run targeted tests then go test ./internal/...; self-review. Write report with APIs, files, tests and limitations to the task report. Controller performs independent review and commit.

### Task 2: Shared source-creation frontend

**Owner/files:** frontend intake agent owns frontend/src/api/sourceImport.ts, frontend/src/composables/useSourceImport.ts, frontend/src/components/import/SourceImportModal.vue and focused tests, frontend/src/utils/sourceIntent.ts and tests. Do not edit App, routes, SideBar, collection views or Copilot; controller wires them.

**Interfaces consumed:** exact Task 1 request/preview/result definitions above. Duplicate the complete TypeScript declarations into api/sourceImport.ts and call registered ImportService via Call.ByName, consistent with api/workbench.ts. TaskService.GetTask(taskId) reports status pending/running/succeeded/failed/cancelled plus message/progress. TaskService.Cancel(taskId) cancels. Inspect generated bindings for actual task shape.

**Interfaces produced:**
```ts
export interface SourceImportOpenRequest {
  kind?: 'project' | 'book' | 'topic'
  sourceType?: 'empty' | 'folder' | 'adopt' | 'files' | 'url'
  source?: string; name?: string; targetFolder?: string
  instruction?: string; autoStart?: boolean
}
export function requestSourceImport(request?: SourceImportOpenRequest): void
// dispatch CustomEvent('notevault:source-import', { detail: request ?? {} })
// SourceImportModal props: open:boolean, request?:SourceImportOpenRequest|null
// emits close(), completed(result:SourceImportResult)
export function parseSourceIntent(text: string): SourceImportOpenRequest | null
```

- [ ] Write component/composable tests for empty book creation, URL/folder inputs, selected/uploaded files, preview, started task progress, cancellation/partial result, retry, conflict counts and workspace-switch isolation. Intent tests require explicit create/import/init verbs and a source; ordinary questions mentioning URLs do not trigger actions.
- [ ] Run failing tests. Implement accessible modal with source tabs, kind/name/destination, optional AI instructions/enrichment, compact optional preview, conflict policy skip/update/copy, picker and drag/drop (read File bytes to bounded base64, no browser filesystem path assumption). Name and destination previews should show the exact Markdown entity to create. Use Wails dialog API already used by the app for local folder choice; manual absolute path remains available. Do not block base import when AI is unconfigured.
- [ ] Background progress continues if modal closes; a shared composable retains the active task per workspace and allows reopening. Poll at a modest interval, stop at terminal states, unsubscribe on disposal, never update a different workspace. Report partial completion honestly. On completion refresh the workbench/file tree once and offer open entity. Avoid retrying writes automatically after unknown network/task outcomes.
- [ ] Recognize explicit Chinese/English source initialization phrases with a single quoted or unambiguous absolute local path or HTTP(S) URL. Infer project/book intent conservatively; otherwise leave it to ordinary chat. AutoStart only for explicit kind+source and valid source text; modal receives and validates request before execution.
- [ ] Run focused tests/typecheck (report unrelated integration errors without editing others' files). Write report. Controller reviews and wires event triggers into global New, empty states, import view and Copilot.

### Task 3: Shared navigation, document browser and contextual AI

**Owner/files:** controller owns frontend/src/stores/navigation.ts and tests, navigation/context composables, App.vue, TitleBar.vue, SideBar.vue, StatusBar.vue, router/index.ts and tests, views except task-owned files, editor state bridge, Copilot/useAIChat and tests, knowledge document-browser component and tests. No new theme variants in views; use shared classes/tokens.

**Interface produced:**
```ts
// Page context derived from route and workspace (not stale activeFile):
interface PageContext { section:'today'|'projects'|'learning'|'vault'; title:string; file:string|null; folder:string|null }
// Navigation setup in App once; title bar shares router back/forward/fallback.
// navigateToFile(router, workspace, path) always supplies query.file.
```

- [ ] Add regressions for Vault filter → editor file → Back, graph → editor → Back, book → chapter → Back, project note → settings → Back twice, no-history fallbacks, workspace isolation and percent filenames. Test real controls where possible; existing route tests that stub all pages cannot prove return behavior.
- [ ] Implement per-workspace history boundary with Vue Router's native state positions, reactive current context/breadcrumbs and shared back/forward. Route afterEach owns history observation. Tab/filter updates use replace. Keep supported page instances keyed by workspace (including graph and collection views); bound the cache and clear on workspace change. Editor tabs update the route with file identities; preserve existing flush/conflict behavior.
- [ ] Remove local back controls and /knowledge return handlers. Use shell navigation on all pages, including settings and library. Make Home Today with workspace. Preserve old URL redirects. Make /library resolve to the real Vault document browser. Store list filters/scroll independently of navigation selection; graph should retain pan/zoom and selection through deactivation.
- [ ] Build real document browser under Vault: name/content entry through existing search, five spaces, list/sort, recent/pinned, selection/open, context-aware New, templates/export and grouped advanced tools. Avoid a second duplicate greeting/task dashboard. Keep every legacy capability reachable. Add functional create/import buttons and adoption for unregistered folders in projects/books.
- [ ] Wire SourceImportModal and requestSourceImport throughout global New, projects/books, old import and Copilot. Derive Copilot context from the current page entity, clear stale request contexts on navigation, pass bounded conversation history to follow-up questions and route citations with explicit files. Recognize explicit source requests through parseSourceIntent; ordinary Answer API remains grounded Q&A.
- [ ] Publish real editor save/error/conflict/cursor state to StatusBar, provide responsive remembered panel widths and preserve tab draft protections. Run focused tests, then integration gates. Independently review the resulting diff.

### Task 4: Theme coverage and compact layout

**Owner/files:** theme agent owns frontend/src/styles/variables.css, themes.css, workbench-collections.css and a new frontend/src/components/settings/ThemePreview.vue. May add a focused theme test if it validates accessibility/behavior, not just CSS text. Do not edit App, SettingsView, other Vue views/components or generated bindings. Controller inserts preview into SettingsView.

**Interfaces consumed:** classes already in collections/Today/task cards, plus new surfaces .global-navigation, .source-import-modal, .document-browser. Prefer semantic tokens and native selectors under data-theme rather than brittle per-page patches. ThemePreview props: theme:'macos'|'winui'|'islands'; selected?:boolean; emits select(theme). It can render an accessible labeled button with a miniature theme-specific shell.

- [ ] Inspect theme definitions and actual current classes. macOS/WinUI already have legacy recipes; extend coverage rather than removing their behavior or copying Islands. Use existing style tokens for background/material/radius/typography/density/focus.
- [ ] Make Mac frosted chrome/segmented controls/soft selection, WinUI Fluent selection marker/pane hierarchy/Segoe/focus, Islands separated dark surfaces/amber/metadata typography. Cover collection buttons/panels/tabs, Today/task metrics, action/sidebar, modal inputs and new browser/navigation. Same controls remain accessible in every theme. Respect reduced motion and compact widths, never rely solely on hue for state.
- [ ] Theme previews visibly show three different materials/geometry. Improve unreadable 10–11px chrome/labels without overflowing. Use solid content surfaces where blur would harm text. Avoid applying Win/macOS decorative window buttons inconsistent with actual host controls.
- [ ] Run frontend typecheck/lint on owned files and note baseline warnings. Capture changes/report rationale; controller verifies all themes on native desktop and compact windows and integrates ThemePreview.

### Task 5: Integration, review and release

**Owner/files:** controller plus independent review agents; only fix findings in affected files. Tracked design spec and implementation plan are included deliberately despite docs/ ignore; test screenshots/logs/runners stay outside repository.

- [ ] Regenerate Wails bindings after API integration. Complete real-navigation regression tests and add import restart/failure edge checks based on review.
- [ ] Run go test ./internal/..., frontend pnpm typecheck, pnpm test, pnpm lint; fix every error/failure and rerun affected gates. Preserve meaningful warnings only if unrelated and accurately report counts.
- [ ] Native QA in isolated user config/workspace: navigation round trips, file identity/dirty draft, filters, graph, source creation/import/adoption/idempotence, progress/retry and three themes at 1440x900 and 1000x720 (also inspect narrow responsive behavior). Collect screenshots outside repository and inspect them. Browser plugin is absent; use installed Playwright through WebView2 CDP and record this reason.
- [ ] Independent broad code review against d239a07 and the spec. Fix material findings and do a scoped re-review. Update ledger with actual evidence.
- [ ] Build CGO_ENABLED=0 wails3 package ARCH=amd64; test NSIS archive with 7z, compare embedded exe SHA256. Commit reviewed code, integrate into main safely without overwriting user changes, push to origin and compare local/remote hashes. Final Chinese report lists main changes, exact checks, commit link and release artifacts with honest remaining format/platform limits.
