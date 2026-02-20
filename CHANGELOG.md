# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- README documenting bd 0.46.0 (Perttulands fork) as a runtime dependency

### Fixed
- Corrected ignored and swallowed errors in `internal/cli/cli.go` for home/hostname resolution, status/read/gc/reservations timestamp parsing, repository path normalization, and force-reserve cleanup paths.
- Corrected ignored and swallowed errors in `internal/store/store.go` for unread cursor read/write handling, sidecar write/close handling, and list calls inside GC/metrics.
- Replaced `return nil, nil` error-path returns with explicit empty slices in list/read helpers to keep empty-state behavior while avoiding hidden error-context bugs.

---

## [0.1.0] - 2026-02-16

Initial release of relay — a filesystem-backed agent-to-agent messaging tool
for the Agora multi-agent system.

### Added
- **Core types**: `Message`, `AgentMeta`, `Reservation`, `Command`, `AgentStatus`
  with 64 KB body size limit (`MaxBodySize`)
- **ULID generation** (`internal/core/ulid.go`): monotonic, thread-safe ULIDs
  for all persistent objects
- **Storage layer** (`internal/store/store.go`):
  - NDJSON append-log inbox with `syscall.Flock` exclusive locking (zero-loss
    concurrent writes)
  - Atomic file operations via temp-file + rename for heartbeats and metadata
  - `O_CREAT|O_EXCL` sidecar files (`.consumed`) for single-consumer command
    claiming
  - SHA-256 content-addressed reservation files (`reservations/{hash}.json`)
  - Prefix-based pattern overlap detection to prevent conflicting reservations
  - Partial trailing-line tolerance in `ReadInbox` for crash recovery
  - GC: archives stale agents (`.stale` suffix), removes expired reservations
- **CLI** (`internal/cli/cli.go`, v0.1.0) with 12 commands:
  - `register` — register an agent with optional program/model/task/bead metadata
  - `heartbeat` — update agent last-seen timestamp
  - `send` — send a message to a named agent; `--broadcast` for all agents
  - `read` — read inbox with cursor-based unread tracking (`--mark-read`),
    filtering by `--from`, `--thread`, `--since`
  - `reserve` — claim a file-path pattern with optional `--repo`, `--exclusive`,
    `--ttl`, `--reason`
  - `release` — release a reservation by pattern
  - `reservations` — list active reservations (own or `--all`)
  - `status` — dashboard of agents, heartbeats, reservations, pending commands
  - `cmd` — inject a slash command into an agent session
  - `gc` — garbage-collect stale agents and expired reservations
  - `wake` — write a wake trigger for the Athena gateway
  - `version` — print version string
- Global flags: `--agent`, `--dir`, `--json`, `--quiet`
- `--tag` flag supports repeatable usage (`--tag a --tag b`) and
  comma-separated values
- Test suite: 90+ test cases across four files with 82.8% coverage; stress test
  verifies zero message loss at 20 K messages across 20 concurrent writers
- `.gitignore` for relay binary and build artifacts

### Notes on abandoned branch (not merged into main)
A parallel branch explored an alternative HTTP API + SQLite architecture
(`pkg/server/server.go`, `pkg/store/store.go`) and included bug fixes
(phantom file cleanup on failed O_EXCL create, mock `execCommand` in wake tests,
`gc_test.go` return-value checks) and a design review (`REVIEW.md`). That branch
was abandoned in favour of the current filesystem/flock approach.
