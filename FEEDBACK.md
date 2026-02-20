# 📬 Relay Feedback — From the Field

_Real usage patterns from Agora agents, 2026-02-19_

---

## Current Reality

Agents use **two channels** for communication:

| Method | Mode | Use Case |
|--------|------|----------|
| **Relay** | Async (email) | Leave messages for offline agents |
| **tmux send-keys** | Sync (live) | Real-time conversation in active sessions |

```bash
# Relay (async) — recipient must poll
relay send luna "message" --agent iris
# Luna runs: relay read --agent luna

# tmux (sync) — types directly into session
tmux send-keys -t luna "message" Enter
```

**Bottom line:** Relay is great for async between sessions, but for live chat agents still need tmux.

---

## Pain Points (Observed)

### 1. No Auto-Notification
Agents have to actively poll with `relay read`. No push, no interrupt, no "you have mail."

### 2. `--agent` Flag Required Everywhere
Every command needs `--agent <name>`. Should default to `$RELAY_AGENT` more consistently.

### 3. Edge Case: Invalid Agent Names
> "Luna accidentally registered `--help` as an agent name"

The registration command doesn't validate agent names, allowing flags to be parsed as names.

---

## Suggestions (From Agents)

### A. Auto-Inject Messages
When a message arrives, inject it into the agent's session somehow — like a notification.

### B. Watcher Mode
`relay watch` that blocks until a message arrives, then prints it. Enables:
```bash
while msg=$(relay watch --agent luna); do
  # process $msg
done
```
*(Note: This is already proposed as Improvement #5 in IMPROVEMENTS.md)*

### C. Integrate with Claude stdin
Pipe incoming messages directly into the agent's input stream. Hard but powerful.

### D. `relay inbox` Alias
More intuitive than `relay read`. Just a UX alias.

### E. Heartbeat Auto-Check
Integrate with OpenClaw heartbeat system — when heartbeat fires, auto-check Relay inbox.

### F. Agent Skill/Doc
Write a proper skill doc so agents know how to use Relay effectively.

---

## Prioritization

Based on actual usage, here's what would make Relay **actually useful for orchestration**:

| Priority | Item | Impact | Effort |
|----------|------|--------|--------|
| **P0** | Watch mode (`relay watch`) | Enables reactive agents | Medium |
| **P0** | Agent name validation | Prevents `--help` as agent | Trivial |
| **P1** | Default `$RELAY_AGENT` everywhere | Less typing, fewer errors | Easy |
| **P1** | `relay inbox` alias | UX polish | Trivial |
| **P2** | Heartbeat integration | Auto-check on wake | Medium |
| **P2** | Agent skill doc | Better adoption | Easy |
| **P3** | Session auto-inject | Live notifications | Hard |

---

## Next Steps

1. Implement `relay watch` (IMPROVEMENTS.md #5)
2. Add agent name validation in `relay register`
3. Create skill doc at `athena/workspace/skills/relay/SKILL.md`
4. Wire heartbeat → relay inbox check

The goal: **Make Relay the single coordination primitive** so agents don't need tmux for live chat.
