# RTK / Token optimization integration status

Updated: 2026-06-13

## Scope

This document tracks the RTK/token optimization integration work for VivuRouter. The focus is backend token optimization first; UI polish and advanced analytics come later.

## Completed tasks

### 1. RTK integration planning

- Created `docs/RTK_INTEGRATION_PLAN.md`.
- Decided not to merge the Rust RTK source directly into the Go gateway in the first phase.
- Chosen integration direction:
  - native Go token optimizer first;
  - optional RTK binary bridge later;
  - UI/settings after backend primitives are safe.

### 2. Native Go token optimizer

Added package:

```text
internal/tokenopt/
```

Implemented:

- rough token estimation via `EstimateTokens`;
- generic text compaction;
- log compaction with repeated-line grouping;
- JSON compaction that preserves shape and truncates large values;
- `CompactToolResult` router that chooses JSON/log/text compaction;
- `Result` metadata with original chars, compact chars and estimated saved tokens.

Tests added and passing for:

- token estimation;
- preserving important error lines;
- log deduplication;
- JSON long value truncation;
- `CompactToolResult` JSON routing.

### 3. Debug/request payload compaction metadata

Updated debug payload flow:

- `buildDebugPayload` now stores compacted prompt/tool result metadata when raw debug is enabled.
- Added fields to `store.RequestLogDebugPayload`:
  - `CompactPrompt`;
  - `CompactToolResult`;
  - `CompactPromptBytes`;
  - `CompactToolResultBytes`;
  - `EstimatedPromptTokensSaved`;
  - `EstimatedToolTokensSaved`;
  - `CompactPromptApplied`;
  - `CompactToolApplied`.
- Raw debug remains opt-in.
- Secret masking happens before compaction.
- Upstream provider payload is not changed by this debug-storage step.

Tests added and passing for:

- compact tool result creation;
- compact prompt creation;
- important error lines preserved in compact tool result.

### 4. Optional `/v1/messages` tool_result compaction

Implemented optional upstream compaction for Anthropic-compatible `/v1/messages` requests.

Behavior:

- Off by default.
- Only compacts blocks with:

```json
{"type":"tool_result","content":"..."}
```

- Does not compact:
  - system prompt;
  - normal user text;
  - assistant text;
  - tool schemas;
  - tool call arguments;
  - short content.
- Clones the request body before mutation.
- Adds marker to compacted content:

```text
[VivuRouter token-optimized tool_result: original X chars -> Y chars, estimated saved Z tokens]
```

Tests added and passing for:

- off by default;
- compaction when enabled;
- original body not mutated;
- non-tool text skipped.

### 5. RTK binary bridge foundation

Added package:

```text
internal/rtkbridge/
```

Implemented:

- OS-aware binary detection:
  - Windows: `rtk.exe`;
  - Linux/macOS: `rtk`.
- Detection order:
  1. `VIVUROUTER_RTK_PATH` or settings path later;
  2. sibling binary next to app;
  3. system `PATH`.
- Detects that Windows `.exe` is not runnable on Linux/macOS.
- Added minimal `Runner.Version(ctx)` to run `rtk --version`.

Verified local bundled binary:

```text
rtk 0.42.4
```

Tests added and passing for:

- OS-specific binary name;
- env path detection;
- sibling detection;
- rejecting `rtk.exe` as runnable on Linux;
- missing binary case.

### 6. Settings UI for end users

Moved token optimization control from env-only to Settings UI.

Added settings fields:

```go
TokenOptimizeToolResults bool
TokenOptimizeMinChars    int
TokenOptimizeMaxChars    int
RTKEnabled               bool
RTKPath                  string
```

Added Settings card:

```text
RTK / Token optimization
```

User can now configure:

- enable/disable tool-result optimization for `/v1/messages`;
- minimum compaction threshold;
- target compact size;
- enable/disable RTK binary bridge;
- optional RTK path.

Gateway now uses DB settings instead of env for `/v1/messages` tool-result compaction.

Settings persist and normalize in both file store and SQLite store.

Tests added/updated and passing for settings persistence and normalization.

### 7. RTK bridge connected to compaction flow

Implemented safe optional RTK use for upstream `/v1/messages` compaction:

- resolves RTK from Settings path, sibling binary, then PATH;
- uses an app/temp controlled input file;
- enforces timeout and input size limit;
- deletes temp input immediately after use;
- runs `rtk read`, `rtk json`, or `rtk log` by detected content mode;
- falls back to native `internal/tokenopt` on RTK errors or unusable binaries;
- tests use fake RTK helpers, not the real RTK binary.

### 8. Advanced token optimization scopes

Added opt-in advanced payload mutation controls. All are off by default:

- `TokenOptimizeSystem` for system prompt content;
- `TokenOptimizeDeveloper` for developer messages/prompts;
- `TokenOptimizeText` for normal message text blocks;
- `TokenOptimizeToolSchemas` for tool descriptions/schema string fields;
- `TokenOptimizeToolCalls` for tool-call input/argument string fields.

Settings UI now warns that these advanced scopes can change model instructions, tool schemas, or tool-call arguments and may cause provider rejection, wrong tool calls, weakened guardrails, or answer drift.

### 9. RTK status API and Settings feedback

Added admin endpoint:

```text
GET /api/admin/rtk/status
```

Response includes enabled/found/source/path/OS/expected binary/can-run-now/version/message.

Settings UI now includes a **Check RTK** button that calls the endpoint, auto-checks when RTK is enabled, and renders status details with OK/warning/error styling. It can also check unsaved `rtk_enabled`/`rtk_path` form values before saving.

### 10. Compact debug display

Request debug modal now exposes separate tabs for:

- Compact prompt;
- Raw prompt;
- Compact tool result;
- Raw tool result.

The request list also shows estimated prompt/tool saved-token hints when compact debug payloads exist.

### 11. Token optimization savings metadata

Request logs now include saved-token metadata:

- `EstimatedTokensSaved`;
- `EstimatedPromptTokensSaved`;
- `EstimatedToolTokensSaved`.

File and SQLite stores persist these fields. The request list can display a compact `Saved` metric.

### 12. Release packaging helpers

Added packaging scripts:

```text
scripts/package-release.ps1
scripts/package-release.sh
```

They build `vivurouter`/`vivurouter.exe` and copy the correct per-OS RTK binary when present:

- Windows: `rtk.exe`;
- Linux/macOS: `rtk`.

## Current verification status

Latest command run:

```powershell
cd vivurouter-go
gofmt -w ...
go test ./...
```

Result: all Go tests pass.

Relevant packages passing:

- `internal/dashboard`
- `internal/gateway`
- `internal/store`
- `internal/rtkbridge`
- `internal/tokenopt`
- `internal/translator`
- `internal/provider`
- `internal/codexoauth`
- `internal/observe`

## Important notes / constraints

- `rtk.exe` works on Windows only.
- Linux/macOS releases need native binary named `rtk`.
- Do not rely on Wine.
- Tool-result optimization is off by default because it mutates upstream payload content.
- Debug compaction metadata only applies when raw debug options are enabled.
- System/developer prompts, normal text, tool schemas and tool call arguments are not compacted unless the user explicitly enables the advanced warning-gated scopes in Settings.
- Secret masking must happen before storing/compacting debug payloads.

## Completion status

Implementation tasks are complete as of 2026-06-13. Remaining work is runtime QA on real environments/providers, not missing repository implementation.

Completed after the original pending list:

- RTK bridge connected to upstream compaction with native fallback.
- RTK status API and Settings feedback implemented.
- Settings can check unsaved `rtk_enabled` / `rtk_path` values before saving.
- `/v1/messages` optimization supports default `tool_result` and warning-gated advanced scopes.
- `/v1/chat/completions` optimization supports OpenAI chat tool messages and warning-gated advanced scopes.
- Request debug modal shows Compact prompt / Raw prompt / Compact tool result / Raw tool result tabs.
- Request logs store estimated saved-token metadata.
- Packaging helper scripts exist for Windows/Linux/macOS RTK layouts.

## Debug checklist for future sessions

### If token optimization appears disabled

1. Distinguish native optimization from RTK bridge:
   - `TokenOptimizeToolResults=true` enables VivuRouter's built-in optimizer.
   - `RTKEnabled=true` only enables the external RTK binary bridge.
   - It is valid for native optimization to be enabled while RTK bridge is disabled.
2. In Settings, **Check RTK** reports both:
   - `Tool-result optimizer`;
   - `RTK bridge`.
3. If the button seems stale, verify `layout.html` cache busters:
   - `/static/app.js?v=20260613-debugtokens1`;
   - `/static/provider-actions.css?v=20260613-usage1`.
4. Run:

```powershell
node --check web/static/app.js
go test ./...
```

### If RTK binary is not detected

1. Windows expects `rtk.exe`; Linux/macOS expect `rtk`.
2. Linux/macOS must not use `rtk.exe` directly.
3. Detection order is Settings path, sibling binary, then PATH.
4. Endpoint to inspect:

```text
GET /api/admin/rtk/status?rtk_enabled=1&rtk_path=<path>
```

Response fields include enabled/found/source/path/os/expected_binary_name/can_run_now/version/message.

### If `/v1/messages` is not compacting

1. Confirm `TokenOptimizeToolResults=true`.
2. Confirm content length is at least `TokenOptimizeMinChars`.
3. Default scope only compacts Anthropic content blocks:

```json
{"type":"tool_result","content":"..."}
```

4. System/developer/text/tool schema/tool call argument optimization requires the separate advanced checkboxes.

### If `/v1/chat/completions` is not compacting

1. Confirm `TokenOptimizeToolResults=true`.
2. Confirm content length is at least `TokenOptimizeMinChars`.
3. Default scope compacts OpenAI Chat tool messages:

```json
{"role":"tool","tool_call_id":"...","content":"..."}
```

4. Advanced chat scopes cover system/developer/text, `tools[].function.description`, nested `tools[].function.parameters` string fields, and `messages[].tool_calls[].function.arguments`.

### If debug modal looks empty

1. Raw debug must be enabled before the request is logged:
   - `SaveRawPrompt`;
   - `SaveRawToolResult`.
2. Compact tabs may be empty when content is below threshold or compaction was not applied.
3. Raw tabs are still lazy-loaded from:

```text
GET /api/admin/request-debug?id=<request_id>
```

### If request token numbers look confusing

Request log token metrics intentionally separate provider usage from debug-storage compaction:

- `Provider input` is the upstream input usage reported by the provider, or VivuRouter's usage estimate when upstream usage is unavailable.
- `Output`, `Total`, `Cached`, and `Reasoning` are provider/upstream usage metrics.
- `Upstream saved` is the estimated token reduction in the request payload before sending upstream. It appears only when VivuRouter marked optimized payload parts.
- `Upstream saved` tooltip also reports optimizer engine (`native`, `rtk`, or combined labels) and optimized part count.
- `Debug raw` is an estimate of the raw debug payload size before compacting the stored debug copy. It is not provider billing.
- `Debug saved` is an estimate of tokens reduced only while storing compact debug/log payloads. It can appear even when `Tool-result optimizer` and `RTK bridge` are both disabled.

Important: debug-storage compaction does not mutate the upstream request. Only `TokenOptimizeToolResults` and the advanced optimization scopes can change the payload sent to the provider.

### Important files

```text
internal/gateway/tool_result_opt.go
internal/gateway/upstream_opt_meta.go
internal/gateway/handler.go
internal/rtkbridge/config.go
internal/rtkbridge/runner.go
internal/dashboard/handlers.go
internal/store/store.go
internal/store/sqlite.go
web/templates/settings.html
web/templates/requests.html
web/templates/layout.html
web/static/app.js
web/static/provider-actions.css
scripts/package-release.ps1
scripts/package-release.sh
```

## Final verification

Latest verification run:

```powershell
node --check web/static/app.js
go test ./...
```

Result: JavaScript syntax OK and all Go tests pass.
