# Relay

![Relay Banner](banner.png)

*Winged sandals. Caduceus of data streams. Zero-loss tally. Always mid-stride. Never standing still.*

---

Relay is a filesystem-backed messaging system for coordinating autonomous agents. No broker, no queue server, no database. Agents register, send messages, claim file reservations, and wake each other up — all through plain files on disk, guarded by `flock` and atomic writes. It exists because when you have multiple AI agents working in the same codebase, they need a way to talk to each other that is dead simple, inspectable with `cat` and `ls`, and impossible to lose messages in. Relay is that.

**Version:** 0.1.0

---

## Origin

In the old stories, Hermes carried messages between gods and mortals, between the living and the dead, between anyone who needed to talk and anyone who needed to listen. He was the fastest thing in any pantheon and he never dropped a message. Not once. The other gods had powers. Hermes had reliability.

Our Hermes wears winged sandals that leave trails of data-light. The caduceus in his hand has two serpents wound around it — except the serpents are data streams, NDJSON flowing in both directions. A messenger satchel full of glowing scrolls, always more coming, never empty. He's the only character in the Agora always caught in motion — speed lines, mid-stride, never standing still. And on his belt, a counter that reads "0 LOST." He's proud of it.

---

## Current Status

All tests pass. The CLI and Go client are functional and used daily in production coordination.

- ✅ Agent registration, heartbeat, card management
- ✅ NDJSON inbox messaging with typed payloads, threading, priorities, tags
- ✅ Unread cursors and blocking `watch` (fsnotify-based)
- ✅ File-pattern reservations with overlap checks, TTL, shared mode
- ✅ Wake orchestration with policy, budget, cooldown, throttle, chain guardrails
- ✅ Command queue (post and consume)
- ✅ Garbage collection (expired reservations, stale agents, old commands)
- ✅ Metrics, activation audit log, spend tracking
- ✅ Task spawning via external `br` and dispatch tooling
- ✅ Go client package (`pkg/client`) with Send, Read, Watch, Card operations
- ✅ No daemon/service mode by design. Relay is filesystem-backed only; there is no `relay serve` command.
- ⚠️ Wake via `openclaw` gateway depends on external services being present and configured
- ⚠️ `spawn` depends on external `br` binary and a dispatch script at the resolved path

---

## Installation

```sh
# From source
cd /path/to/relay
go build -o relay ./cmd/relay

# Or install to GOBIN
go install ./cmd/relay
```

## Quick Start

```sh
# Register agent and start heartbeating
relay --agent hestia register hestia --task "working on pol-abc1"
relay --agent hestia heartbeat --task "working on pol-abc1"

# Send a message
relay send athena "task complete" --type task_result

# Read inbox
relay read --unread --mark-read

# Block until new messages arrive
relay watch --agent luna
relay watch --agent luna --loop

# Check system state
relay status
```

## CLI Commands

### Global Flags

These flags are recognized anywhere in the argument list and stripped before command dispatch.

| Flag | Default | Description |
|------|---------|-------------|
| `--agent <name>` | `RELAY_AGENT` env, then hostname | Acting agent identity |
| `--dir <path>` | `RELAY_DIR` env, then `~/.relay` | Relay root data directory |
| `--json` | `false` | JSON output mode (where implemented) |
| `--quiet` | `false` | Suppress non-essential text output (coverage varies by command) |

---

### `relay register <name> [flags]`

Register or update agent metadata and write an initial heartbeat.

| Flag | Description |
|------|-------------|
| `--program <p>` | Agent program name |
| `--model <m>` | Agent model identifier |
| `--task <t>` | Current task; sets card `current_task` and `status=working` |
| `--bead <b>` | Current bead ID |
| `--gateway-url <url>` | Wake gateway URL |
| `--gateway-token <token>` | Wake gateway token |
| `--session-key <key>` | Session key |
| `--skills <s1,s2>` | Comma-separated skills list for agent card |
| `--budget <N>` | Daily wake budget limit (integer >= 0) |
| `--cooldown <dur>` | Cooldown duration between wakes (must be > 0) |

---

### `relay heartbeat [flags]`

Update the acting agent's heartbeat timestamp.

| Flag | Description |
|------|-------------|
| `--task <t>` | Update current task; sets card `current_task=t`, `status=working` |
| `--idle` | Clear current task and set card `status=idle` |

If both `--task` and `--idle` are passed, `--task` wins.

---

### `relay send <to> <message> [flags]`

Send one message to one recipient.

| Flag | Default | Description |
|------|---------|-------------|
| `--subject <text>` | (body truncated to 80 chars) | Message subject |
| `--thread <id>` | | Thread ID |
| `--priority <p>` | `normal` | Message priority |
| `--tag <t1,t2>` | | Comma-separated tags |
| `--type <type>` | | Typed message: `task_result`, `request`, `alert`, `status`, `chat` |
| `--payload <json>` | | Raw JSON payload (validated at send time) |
| `--wake` | | Run wake workflow after send |
| `--no-wake` | | Disable wake even if `--wake` is present |
| `--chain-id <uuid>` | | Propagate existing wake chain ID |
| `--max-depth <n>` | `3` | Wake chain max depth |

#### Broadcast mode

```sh
relay send --broadcast <message> [flags]
```

Sends to all registered agents except the sender. Same message flags apply. When `--wake` is used in broadcast mode, a generic wake is issued (no single target).

---

### `relay read [flags]` / `relay inbox [flags]`

Read inbox for the acting agent. (`inbox` is an alias for `read`.)

| Flag | Default | Description |
|------|---------|-------------|
| `--from <agent>` | | Filter by sender |
| `--thread <id>` | | Filter by thread ID |
| `--type <type>` | | Filter by message type |
| `--since <time>` | | Lower-bound filter: duration (`1h`), RFC3339, or date (`YYYY-MM-DD`) |
| `--last <n>` | `20` | Last N messages |
| `--unread` | | Read from unread cursor offset |
| `--mark-read` | | Update cursor to EOF after read |

---

### `relay watch [flags]`

Block for new inbox appends. Returns newly appended messages since last offset.

| Flag | Description |
|------|-------------|
| `--loop` | Keep watching after first batch instead of returning |

In `--json` mode, outputs one JSON message object per line/event.

---

### `relay status [flags]`

Show agents, reservations, and pending commands.

| Flag | Default | Description |
|------|---------|-------------|
| `--stale <dur>` | `5m` | Stale heartbeat threshold for agent status display |

JSON output fields: `agents`, `reservations`, `commands`.

---

### `relay reserve <pattern> [flags]`

Create a reservation for a repo and glob pattern.

| Flag | Default | Description |
|------|---------|-------------|
| `--repo <path>` | cwd | Target repository (normalized to absolute path) |
| `--ttl <dur>` | `1h` | Reservation lifetime |
| `--reason <text>` | | Reason string |
| `--check` | | Overlap check only; do not actually reserve |
| `--force` | | Override conflicts by deleting existing reservation and retrying |
| `--shared` | | Set reservation to non-exclusive |

---

### `relay release <pattern> [--repo <path>]`

Release one reservation owned by the acting agent.

| Flag | Default | Description |
|------|---------|-------------|
| `--repo <path>` | cwd | Repository scope |

#### Release all

```sh
relay release --all
```

Releases all reservations owned by the acting agent.

---

### `relay reservations [flags]`

List reservations.

| Flag | Description |
|------|-------------|
| `--repo <path>` | Filter by repository absolute path |
| `--agent <name>` | Filter by owner |
| `--expired` | Include expired reservations (hidden by default) |

---

### `relay wake [text] [flags]`

Trigger the wake mechanism directly.

| Flag | Description |
|------|-------------|
| `--method <gateway\|file>` | Force wake method: `gateway` (requires `~/.openclaw/workspace/scripts/wake-gateway.sh`) or `file` (writes trigger files to relay data dir). Without `--method`, auto mode tries gateway then file fallback. |

---

### `relay cmd <target-session> <command> [args...] [flags]`

Post a command object to the command queue.

| Flag | Description |
|------|-------------|
| `--wake` | After posting, issue a generic wake with message `command: <cmd> <args>` |

---

### `relay gc [flags]`

Garbage-collect relay state: removes expired reservations, old consumed commands, and archives stale agents.

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run` | | Print what would be removed without removing it |
| `--expired-only` | | Skip stale-agent archiving |
| `--stale <dur>` | `30m` | Stale threshold for agent archival |

---

### `relay metrics [flags]`

Aggregate counts of agents, messages, reservations, and commands.

| Flag | Default | Description |
|------|---------|-------------|
| `--stale <dur>` | `5m` | Stale threshold for alive/stale agent split |

---

### `relay card [agent] [flags]`

Show the card for a named agent or self (default).

| Flag | Description |
|------|-------------|
| `--all` | List all agent cards |

---

### `relay throttle [flags]`

Manage city-wide wake throttle and per-agent budget overrides. Requires one of:

| Flag | Description |
|------|-------------|
| `--suspend-all` | Set city throttle suspended=true, stamped by acting agent |
| `--resume` | Set city throttle suspended=false |
| `--status` | Display current throttle state |
| `--set-budget <agent> <N>` | Write per-agent budget override |

---

### `relay policy [flags]`

Manage activation policy (`from -> to` wake authorization). Requires one of:

| Flag | Description |
|------|-------------|
| `--show` | Display current policy |
| `--allow <from> <to>` | Append an allow rule |
| `--deny <from> <to>` | Append a deny rule |
| `--reset` | Reset to default deny policy |

Deny rules take precedence over allow rules.

---

### `relay spend [flags]`

Aggregate delivered wake activations from the activation log by target agent. Requires one of:

| Flag | Description |
|------|-------------|
| `--today` | Aggregate today |
| `--week` | Aggregate current week (Monday start) |
| `--target <name>` | Aggregate all-time for one target |

---

### `relay log [flags]`

Display activation log entries. Requires one of:

| Flag | Default | Description |
|------|---------|-------------|
| `--chain <id>` | | Filter by wake chain ID |
| `--tail [N]` | `20` | Show last N entries |

---

### `relay spawn [flags]`

Create a task issue via `br`, dispatch a worker script, and optionally wait for a result and notify.

| Flag | Required | Description |
|------|----------|-------------|
| `--repo <path>` | Yes | Target repository for result file lookup |
| `--agent <type>` | Yes | Agent type: `codex`, `claude:opus`, `claude:sonnet`, `claude:haiku` |
| `--prompt <text>` | Yes | Dispatch prompt |
| `--title <text>` | No | Task title (defaults to prompt truncated to 50 runes) |
| `--beads-dir <path>` | No (recommended) | Directory where `br create` runs (pass your project `.beads` path explicitly) |
| `--wait` | No | Wait for `state/results/<bead>.json` up to 30 minutes |
| `--notify <agent>` | No | Send relay message on completion |

---

### `relay version`

Print `relay <version>`.

---

### `relay help`

Print usage text with command list.

---

## Configuration

### Environment Variables

| Variable | Description |
|----------|-------------|
| `RELAY_DIR` | Relay root directory override |
| `RELAY_AGENT` | Acting agent identity override |
| `DISPATCH_SCRIPT` | Explicit dispatch script path for `spawn` |
| `ATHENA_WORKSPACE` | Fallback workspace path used by `spawn` for `br create` when `--beads-dir` is not provided |
| `HOME` | Used for default relay dir expansion, wake gateway script lookup, BR binary lookup, dispatch/workspace fallback paths |

### On-Disk State Files

All paths are under the relay root (`~/.relay` by default) unless noted.

| Path | Contents |
|------|----------|
| `agents/<name>/meta.json` | Agent metadata and gateway/session info |
| `agents/<name>/card.json` | Agent capabilities, status, last_seen, budget, cooldown defaults |
| `agents/<name>/heartbeat` | RFC3339 heartbeat timestamp |
| `agents/<name>/heartbeat.stale` | Stale marker created by GC |
| `agents/<name>/inbox.jsonl` | NDJSON inbox |
| `agents/<name>/cursor` | Unread byte offset cursor |
| `agents/<name>/budget.json` | Daily wake budget state |
| `agents/<name>/cooldown.json` | Last wake time and cooldown seconds |
| `reservations/<sha256>.json` | Reservation records |
| `commands/<ulid>.json` | Command queue entries |
| `commands/<ulid>.consumed` | Command claim marker |
| `chains/<uuid>.json` | Wake chain state and hops |
| `wake/trigger` | File-based wake trigger timestamp |
| `wake/last-message` | Last wake text (file wake mode) |
| `activation-policy.toml` | Wake authorization policy |
| `throttle.json` | Global throttle and per-agent budget overrides |
| `activation-log.jsonl` | Wake activation audit log |

### Defaults and Constants

| Setting | Default |
|---------|---------|
| Relay root | `~/.relay` |
| Acting agent | hostname |
| Message body max | 65536 bytes |
| Auto subject truncation | 80 chars |
| Read default last count | 20 |
| Log default tail count | 20 |
| Reserve default TTL | `1h` |
| Status stale threshold | `5m` |
| GC stale threshold | `30m` |
| Wake chain max depth | `3` |
| Budget default per-day limit | `20` |
| Cooldown default | `300s` |
| Spawn poll interval | `2s` |
| Spawn poll timeout | `30m` |

## Go Client Package

The `pkg/client` package provides a programmatic API:

```go
c, err := client.NewClient("~/.relay")

c.Send(to, body)
c.SendTyped(to, body, msgType, payload)
c.Read(client.ReadOpts{...})
c.Watch()             // blocks until new messages arrive
c.UpdateCard(card)
c.GetCard(agent)
c.ListCards()
```

Directory resolution: explicit arg, then `RELAY_DIR`, then `~/.relay`.
Agent resolution: `RELAY_AGENT`, then hostname.

## Dependencies

### Required

None. All relay operations are filesystem-based. No external services needed for core functionality.

### Optional External Binaries

| Binary | Purpose |
|--------|---------|
| `openclaw` | Direct gateway wake injection |
| `systemctl` (user mode) | Fallback service wake |
| `br` (or `~/.cargo/bin/br`) | Used by `spawn` and budget-exceeded wake branch |
| `~/.openclaw/workspace/scripts/wake-gateway.sh` | Wake gateway script (wake command and fallback) |
| Dispatch script (`DISPATCH_SCRIPT` or `~/athena/workspace/scripts/dispatch.sh`) | Used by `spawn` |

### Go Module Dependencies

- `github.com/fsnotify/fsnotify v1.7.0` — Filesystem watch for inbox watch
- `github.com/oklog/ulid/v2 v2.1.1` — ULID generation
- `golang.org/x/sys v0.4.0` — Low-level system calls (flock support)

---

## Part of Polis

Relay is the nervous system of a larger ecosystem of tools that coordinate autonomous agents.

| Tool | Repo |
|------|------|
| Ergon (work orchestration) | [ergon-work-orchestration](https://github.com/Perttulands/ergon-work-orchestration) |
| Cerberus (gate) | [cerberus-gate](https://github.com/Perttulands/cerberus-gate) |
| Chiron (trainer) | [chiron-trainer](https://github.com/Perttulands/chiron-trainer) |
| Learning Loop | [learning-loop](https://github.com/Perttulands/learning-loop) |
| Senate | [senate](https://github.com/Perttulands/senate) |
| Beads | [beads-polis](https://github.com/Perttulands/beads-polis) |
| Truthsayer | [truthsayer](https://github.com/Perttulands/truthsayer) |
| UBS (bug scanner) | [ultimate_bug_scanner](https://github.com/Perttulands/ultimate_bug_scanner) |
| Oathkeeper | [horkos-oathkeeper](https://github.com/Perttulands/horkos-oathkeeper) |
| Argus (watcher) | [argus-watcher](https://github.com/Perttulands/argus-watcher) |
| Polis Utils | [polis-utils](https://github.com/Perttulands/polis-utils) |

[Argus](https://github.com/Perttulands/argus-watcher) watches the server. [Truthsayer](https://github.com/Perttulands/truthsayer) watches the code. [Oathkeeper](https://github.com/Perttulands/horkos-oathkeeper) watches the promises. Relay carries the messages. Zero messages lost.

The [mythology](https://github.com/Perttulands/athena-workspace/blob/main/mythology.md) has the full story.

## License

MIT
