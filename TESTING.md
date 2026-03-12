# TESTING.md — relay

Rubric: [TEST_RUBRIC.md](/home/polis/tools/TEST_RUBRIC.md)

## Rubric Scores

| Dimension | Before | After | Delta |
|---|---|---|---|
| E2E Realism | 3 | 4 | +1 |
| Unit Behaviour Focus | 3 | 3.5 | +0.5 |
| Edge Case & Error | 3 | 4 | +1 |
| Isolation & Reliability | 3 | 3 | 0 |
| Regression Value | 3 | 4 | +1 |
| **Total** | **15** | **18.5** | **+3.5** |

**Grade: B** (Good, with known gaps)

## Assessment Per Dimension

### 1. E2E Realism — 4/5
E2E now covers the most important agent workflows end-to-end through the compiled binary: send with all flags, read with JSON parse, typed messages with payloads, heartbeat/card lifecycle, reserve/conflict/force, command queue, broadcast, GC. Missing: `watch` (blocking, hard to E2E), `spawn` (needs mocked external tooling), concurrent E2E usage.

### 2. Unit Behaviour Focus — 3.5/5
Core logic tests are strong: send/read roundtrip with all fields, unread cursor, concurrent writes, reservation conflicts, overlap detection. Several older edge_test.go tests still only check exit codes (e.g. `TestReserveQuiet`, `TestGCQuiet`) — they'd pass even if the feature was broken. These are coverage-only tests from a prior session and were not removed to avoid churn, but they have low regression value.

### 3. Edge Case & Error — 4/5
Now covers: mid-file inbox corruption recovery, concurrent reserve race (O_CREAT|O_EXCL), subject truncation exact boundary (79/80/81), GC preserves active reservations, expired reservations ignored in overlap checks, wrong-agent release reports owner name, first-unread-read without cursor file, unparseable ExpiresAt treated as expired (GC removes, overlap ignores). Still missing: flock contention failure, disk-full during atomic write.

### 4. Isolation & Reliability — 3/5
All tests use temp dirs. **Known issues**: `setup()` in cli_test.go uses `os.Setenv`/manual restore instead of `t.Setenv` — if a test panics, env leaks. Watch tests use `time.Sleep(100ms)` — flaky under CI load. `captureRun` redirects `os.Stdout` — not parallel-safe. `withMockExec` mutates package-level var — not parallel-safe. These are pre-existing; fixing them requires refactoring the CLI test harness.

### 5. Regression Value — 4/5
Hard to break message delivery, reservation conflicts, broadcast exclusion, concurrent write safety, cursor mechanism, field roundtrip, or GC safety without a test failing. Remaining gap: error message content that agents parse (e.g. "not registered", "conflict") is only partially asserted.

## What the Suite is MISSING

**Critical gaps:**
- **No test for `atomicWrite` failure paths** (53% coverage) — this is the core durability mechanism. A subtle bug here could cause silent data loss. Hard to test without filesystem fault injection.
- **No test for flock contention failure** — if `syscall.Flock` returns an error (e.g. NFS, unsupported fs), messages could be lost. The code doesn't retry.
- ~~**`isExpired` returns false for unparseable timestamps**~~ — **Fixed**: `isExpired` now returns `true` for unparseable timestamps. Tested by `TestIsExpiredMalformedTimestamp`, `TestIsExpiredEmptyTimestamp`, `TestGCRemovesUnparsableReservation`, and `TestCheckOverlapIgnoresUnparsableReservation`.

**Important gaps:**
- **No parallel-safe CLI tests** — `os.Setenv`, `os.Stdout` redirect, package-level mock prevent `t.Parallel()`. A refactor to use dependency injection would fix this.
- **Watch race window untested** — messages arriving between `os.Stat(inbox)` (line 313) and `watcher.Add(inbox)` (line 324) could be lost. The window is small but real.
- **No test for `spawn` end-to-end through binary** — spawn depends on external `br` and `dispatch.sh` which can't be mocked in E2E tests.
- **No fuzz testing** — inbox NDJSON parsing tolerates malformed lines, but no systematic fuzzing of message shapes.

**Low-priority gaps:**
- `cmd/relay/main.go` at 0% — uncoverable without integration testing the `main()` entry point.
- `resolveDispatchScript` / `resolveWorkspaceDir` still depend on explicit environment configuration (`DISPATCH_SCRIPT`, `ATHENA_WORKSPACE`).

## Changelog

### 2026-02-28 — Agent: Claude Opus 4.6
- **Fixed**: `ReadInbox` with `Unread: true` on first read (no cursor file) returned an error instead of all messages. Root cause: `os.IsNotExist()` does not unwrap `fmt.Errorf %w` errors — replaced with `errors.Is(readErr, fs.ErrNotExist)` in store.go line 246.
- **Added**: `TestSendReadRoundtripPreservesAllFields` — verifies every Message field (ID, TS, From, To, Subject, Body, Thread, Priority, ReplyTo, Tags, Type, Payload) survives the store send→read roundtrip. Would catch any field being dropped by marshal/unmarshal changes.
- **Added**: `TestConcurrentReserveSamePattern` — 10 goroutines racing to reserve the same pattern; verifies exactly 1 wins and 9 get conflict errors. Tests the O_CREAT|O_EXCL atomicity guarantee.
- **Added**: `TestInboxCorruptionMidFileRecovery` — corrupt JSON line between two valid messages; verifies both valid messages are recovered. Simulates disk corruption or partial writes.
- **Added**: `TestGCPreservesActiveReservations` — verifies GC only removes expired reservations and active ones survive untouched. Critical safety property.
- **Added**: `TestSubjectAutoTruncationBoundary` — tests exact 79/80/81/200 char bodies to document the 80-char truncation contract agents depend on.
- **Added**: `TestReleaseByWrongAgentReportsOwner` — verifies error message includes both the actual owner and the requesting agent name. Agents parse these errors.
- **Added**: `TestCheckOverlapIgnoresExpiredReservations` — verifies expired reservations don't create false conflicts that would block new agents.
- **Added**: `TestReadUnreadFirstTimeNoCursor` — exercises first `--unread` read on a fresh agent. **Caught the `os.IsNotExist` bug** (see fix above).
- **Added**: `TestMessageOrderingPreserved` — verifies sequential sends produce chronologically ordered reads. Agents depend on this for conversation flow.
- **Added**: `TestGCStaleAgentRenamesHeartbeat` — verifies GC renames heartbeat→heartbeat.stale (the mechanism Athena uses to detect dead agents).
- **Added**: `TestForceReserveActuallyOverrides` (CLI) — verifies --force removes old reservation and installs new one, checking store state not just exit code.
- **Added**: `TestSendReadRoundtripThroughCLI` — full CLI send with all flags → read --json → verify all fields. Tests the complete flag→message→inbox→output pipeline.
- **Added**: `TestBroadcastDeliveryAndSenderExclusion` (CLI) — verifies broadcast reaches workers and sender is excluded, checking store state directly.
- **Added**: `TestUnreadCursorSurvivesSessions` (CLI) — verifies mark-read cursor persists across read calls via CLI.
- **Added**: `TestStatusJSONContainsAllExpectedFields` (CLI) — verifies JSON status output structure that monitoring tools parse.
- **Added**: `TestE2E_FullAgentWorkflow` — realistic multi-agent E2E: register, reserve, typed send with payload, read with JSON verify, status, release.
- **Added**: `TestE2E_HeartbeatAndCard` — heartbeat→card lifecycle through binary: task update, idle clear, JSON verification.
- **Added**: `TestE2E_ReserveConflictAndForce` — conflict detection and force-override through binary.
- **Added**: `TestE2E_CommandPostAndList` — command queue through binary: post, verify in status.
- Coverage delta: 84.4% → ~85% (meaningful: 19 new tests covering real behaviours, 1 bug fix)

### 2026-02-28 — Agent: Claude Opus 4.6 (pol-4jwa)
- **Fixed**: `isExpired` returned `false` for unparseable `ExpiresAt` timestamps, causing reservations with malformed timestamps to never be garbage-collected and permanently block their patterns. Changed to return `true` (treat as expired). Consistent with `Metrics()` which already counted unparseable as expired.
- **Updated**: `TestIsExpiredMalformedTimestamp` — now asserts `true` (was asserting the buggy `false` behavior).
- **Added**: `TestIsExpiredEmptyTimestamp` — empty string ExpiresAt returns true.
- **Added**: `TestGCRemovesUnparsableReservation` — integration test: GC removes reservation with malformed timestamp while preserving active reservations.
- **Added**: `TestCheckOverlapIgnoresUnparsableReservation` — unparseable reservation doesn't create false conflicts blocking new agents.
