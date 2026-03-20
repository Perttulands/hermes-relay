# Relay — Product Requirements Document

**Version:** 0.2.0 (planned)
**Last updated:** 2026-03-16

---

## Existing Capabilities (v0.1.0)

Relay is a filesystem-backed messaging system for autonomous agent coordination. No broker, no daemon, no database. All state lives in `~/.relay/` as plain files.

### Implemented

- Agent registration, heartbeat, card lifecycle
- NDJSON inbox messaging with typed payloads, threading, priorities, tags
- Unread cursors and blocking `watch` (fsnotify)
- File-pattern reservations with overlap checks, TTL, shared mode
- Wake orchestration with policy, budget, cooldown, throttle, chain guardrails
- Command queue (post and consume)
- Garbage collection (expired reservations, stale agents, old commands)
- Metrics, activation audit log, spend tracking
- Task spawning via `work run` wrapper
- Go client package (`pkg/client`)
- Activation policy (allow/deny rules with trust levels)
- Global throttle and per-agent budget overrides
- Broadcast messaging

---

## Change 1: Default-Allow Messaging Policy

### Problem

The activation policy defaults to `deny`, requiring explicit allow rules for every agent pair. This creates friction (e.g., hestia->athena blocked) without providing meaningful security. All agents run under the same user account with full filesystem and tmux access to each other.

### Security Analysis

The relay system is filesystem-backed, CLI-only, no network service. All state under `~/.relay/` is owned by the `polis` user. An attacker who can invoke `relay send` already has shell access as `polis` — they can directly write to any inbox, read any file, or inject into any tmux session. The policy adds no security boundary beyond what filesystem permissions already provide.

The graduation system (trust levels 0-4) and harbour audit log remain in place for tracking genuinely external agents if any are introduced in the future. All current agents (Claude Code, Codex) are internal (trust_level 4). These controls are orthogonal to the default policy — they govern audit logging and `pause-external`, not basic send permissions.

### Implementation

1. Change `DefaultPolicy()` in `internal/store/policy.go` from `"deny"` to `"allow"`.
2. Update `activation-policy.toml` to `default = "allow"` (done 2026-03-16).
3. Update `cmdPolicy --reset` to reset to default-allow instead of default-deny.
4. Update relevant tests.

**Status**: Policy file updated. Code change pending.

---

## Feature: `work send` Wake Method

### Problem

The primary way agents push prompts into each other's sessions today is ad-hoc `tmux send-keys`. This is the #1 source of agent coordination failures:
- Agents frequently fail to send enough Enter keys, leaving paste buffers unsubmitted
- Agents misdiagnose unsubmitted prompts as stuck agents and kill sessions
- The `sleep` / `send-keys` / `capture-pane` pattern is fragile and undocumented outside the coding-team skill

Relay has a wake chain (gateway injection -> systemctl start -> file-based wake) but no tmux session delivery method. Meanwhile, `work` already has a tested tmux injection system via `worker.SendPrompt()` which uses the robust `load-buffer`/`paste-buffer` pattern with double Enter and timing delays (`internal/worker/worker.go:254`).

### Prerequisites

**`work send` must be upgraded before this integration ships.**

Currently, `work send` (`internal/cli/send.go:93`) uses `tmux send-keys -l` — the same fragile method agents use ad-hoc. The robust `load-buffer`/`paste-buffer` implementation lives in `worker.RealTmuxClient.sendPrompt()` (`worker.go:254-293`) and is only used by `work run` internally.

Before relay integrates, `work send` must be updated to call `worker.SendPrompt()` instead of its own `sendPrompt()`. This is a ~5 line change in `send.go`: import the worker package and call `worker.SendPrompt(session, prompt)`.

### Solution

Add `work send` as a wake method in relay's existing wake chain, between gateway injection and systemctl fallback. Relay stores the target agent's tmux session name in meta.json; when `relay send --wake` fires, it resolves the session and delegates injection to `work send`.

### Requirements

1. **Agent registration with tmux session**: `relay register hestia --tmux-session polis-boss` stores `tmux_session` in `AgentMeta` / `meta.json`. This targets long-lived named agent sessions (e.g., `polis-boss`, `polis-builder-1`), not ephemeral per-run sessions created by `work run`.

2. **Wake chain addition**: In the targeted wake block of `internal/cli/cli.go`, after gateway injection and before systemctl fallback, add:
   ```
   gateway injection (if gateway_url set)
   -> work send (if tmux_session set)     <- NEW
   -> systemctl start (fallback)
   -> file-based wake (last resort)
   ```

3. **Delegation logic** (~20 lines):
   a. Read target agent's `meta.json` to get `tmux_session`
   b. Write message body to a temp file (avoids shell-quoting issues with newlines, quotes, metacharacters in bodies up to 64 KiB)
   c. Set `TMUX_TMPDIR` in the subprocess environment (from process environment inheritance — no per-agent meta field needed)
   d. Exec `work send <session> --file /tmp/relay-wake-XXXX.txt`
   e. Clean up the temp file
   f. If `work send` succeeds (exit 0): log activation as `"delivered"` with reason `"work send"`, update cooldown
   g. If `work send` fails (non-zero exit): log activation as `"injection_failed"` with reason `"work send failed: <stderr>"`, fall through to systemctl

4. **No new CLI commands in relay**: No `relay inject`. That's `work send`. Relay is transport + wake policy; `work` is session management.

5. **No direct tmux commands in relay**: Relay does not run `tmux has-session` or any tmux commands itself. Session validation is `work send`'s responsibility. Relay interprets the exit code and falls through on failure.

### On-Disk Changes

**`AgentMeta`** in `internal/core/types.go` — add field:
```go
TmuxSession string `json:"tmux_session,omitempty"`
```

**`meta.json`** example:
```json
{
  "name": "hestia",
  "tmux_session": "polis-boss"
}
```

### CLI Surface

```bash
# Register with tmux session
relay register hestia --tmux-session polis-boss

# Send + wake (auto-delegates to work send for tmux injection)
relay send hestia "CHECK-IN: status update please" --wake

# For direct session injection without relay message, use work directly:
work send polis-boss "CHECK-IN 3/8: check builders, push work forward."
work send polis-boss --file /tmp/task.md
```

### Non-Goals

- Relay does not implement tmux injection itself — `work` owns that
- Relay does not spawn tmux sessions
- Relay does not capture panes for monitoring
- No `relay inject` command — use `work send` directly
- This does not cover ephemeral `work run` sessions — only long-lived named sessions

### Dependency

Requires `work` binary on PATH. If `work` is not available, the wake chain falls through to systemctl (same pattern as gateway injection fallback when `openclaw` is absent).

### Test Plan

1. **Unit: delegation call** — mock `work` binary, verify `work send <session> --file <path>` is called with correct args when `tmux_session` is set in meta
2. **Unit: work missing fallthrough** — verify fall-through to systemctl when `work` binary is not on PATH
3. **Unit: work send failure fallthrough** — verify fall-through to systemctl when `work send` returns non-zero exit
4. **Integration: end-to-end** — `relay register --tmux-session`, then `relay send --wake`, verify activation log shows `reason: "work send"` (or appropriate failure reason)

---

## Roadmap

| Priority | Item | Scope | Rationale |
|----------|------|-------|-----------|
| P0 | Default-allow policy | ~5 lines in policy.go + test updates | Unblocks all agent communication immediately |
| P0.5 | Upgrade `work send` to use `worker.SendPrompt()` | ~5 lines in work/internal/cli/send.go (separate repo) | Prerequisite for P1; delivers robustness improvement |
| P1 | `work send` wake method in relay | ~20 lines in cli.go + 1 field in types.go + registration flag + 4 tests | Reliable session injection via relay wake chain |
