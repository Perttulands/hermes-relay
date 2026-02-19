# Relay

Relay is the agent messaging backbone for the Agora system. It handles inter-agent communication, command routing, and message delivery.

## Build

```bash
go build ./...
```

## Dependencies

### Beads (bd CLI)

Relay uses beads for tracking message delivery and agent coordination tasks.

- Required version: **0.46.0**
- Fork: [Perttulands/beads](https://github.com/Perttulands/beads) (branch `v0.46.0-stable`)
- Install: `go install github.com/Perttulands/beads/cmd/bd@v0.46.0`
- Verify: `bd --version` should show `bd version 0.46.0`
