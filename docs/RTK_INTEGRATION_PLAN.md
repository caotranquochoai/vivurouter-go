# RTK integration plan for VivuRouter

## Goal

Integrate RTK concepts into VivuRouter as a token optimization layer. The first implementation focuses on backend/token savings, not UI.

RTK is a Rust CLI proxy that reduces LLM context usage by compacting command output. VivuRouter is a local AI gateway/proxy. The useful integration point is therefore not provider routing itself, but large tool results, debug payloads, logs, JSON payloads, and command output that may flow through `/v1/messages` or request debugging.

## Direction

Do not merge the Rust codebase directly into the Go gateway in the first phase. Keep RTK as:

1. A reference implementation for compaction strategies.
2. An optional external binary bridge for later release builds.
3. A possible source of analytics via `rtk gain --format json` later.

The first production-safe step is a native Go token optimization package inside VivuRouter.

## Phases

### Phase 1 — Native Go token optimizer

Add `internal/tokenopt` with:

- rough token estimation;
- generic text compaction;
- log compaction;
- JSON compaction;
- result metadata: original chars, compact chars, estimated tokens saved, applied flag, reason.

This phase does not change routing behavior. It gives the gateway reusable primitives that can be applied safely in later phases.

### Phase 2 — Debug/log compaction

Apply token optimization to debug storage and request log display metadata first, not upstream payloads.

Rules:

- raw debug remains opt-in;
- compacted debug can be stored alongside raw or used when raw is disabled;
- never log secrets intentionally;
- preserve error/warning/failure lines.

### Phase 3 — Anthropic tool_result compaction

Add optional compaction for large `tool_result` content in `/v1/messages` requests.

Status: implemented as an opt-in backend feature.

Configuration is stored in Settings and exposed in the Settings page:

```text
token_optimize_tool_results=true
token_optimize_min_chars=12000
token_optimize_max_chars=12000
rtk_enabled=false
rtk_path=
```

The old environment-only workflow is no longer required for end users.

Safety rules:

- off by default;
- only compact content blocks with type `tool_result`;
- do not compact system/developer instructions;
- do not compact tool schemas or tool call arguments;
- skip short content;
- clone the request body before mutation;
- add a small marker with original/compact size and estimated tokens saved.

### Phase 4 — Optional RTK binary bridge

Status: detection/runner foundation implemented in `internal/rtkbridge`.

Current behavior:

- detect `rtk` via `VIVUROUTER_RTK_PATH`;
- otherwise detect sibling binary next to the app;
- otherwise detect from `PATH`;
- binary name is OS-aware:
  - Windows: `rtk.exe`;
  - Linux/macOS: `rtk`;
- Windows `.exe` is marked as not runnable on Linux/macOS, so releases must ship a native binary per OS.

Later bridge calls should:

- call RTK with timeouts and input size limits;
- use temp files only inside controlled app data directory;
- delete temp files immediately;
- fallback to native Go compaction on error.

### Phase 5 — Release packaging

Update release scripts to optionally build/copy `rtk.exe` or `rtk` next to `vivurouter`.

## Initial API proposal

```go
type Options struct {
    MaxChars       int
    MinChars       int
    PreserveErrors bool
}

type Result struct {
    OriginalChars            int
    CompactChars             int
    EstimatedOriginalTokens  int
    EstimatedCompactTokens   int
    EstimatedSavedTokens     int
    Applied                  bool
    Reason                   string
    Text                     string
}

func EstimateTokens(s string) int
func CompactToolResult(input string, opts Options) Result
func CompactText(input string, opts Options) Result
func CompactLog(input string, opts Options) Result
func CompactJSON(input string, opts Options) Result
```

## Non-goals for the first implementation

- No UI changes.
- No automatic command hook installation.
- No direct Rust-to-Go rewrite.
- No default upstream payload mutation.
- No compaction of prompts/instructions.

## Security notes

- Never write secrets to logs.
- If a future RTK bridge uses temp files, keep them in app-controlled temp storage and delete immediately.
- Preserve error lines and surrounding context.
- Avoid compacting content where exact byte-for-byte fidelity matters.

## Current status

Phase 1 is being implemented first as `internal/tokenopt`.
