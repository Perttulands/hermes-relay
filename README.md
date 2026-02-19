# Relay

Relay is the agent messaging backbone for the Agora system. It handles inter-agent communication, command routing, and message delivery.

## Quick CLI

```bash
# Register an agent
relay register luna

# Send and read messages
relay send luna "ship it" --agent iris
relay read --agent luna
relay inbox --agent luna   # alias for read

# Block until new message(s) arrive
relay watch --agent luna
relay watch --agent luna --loop
```

Commands that need agent identity resolve it in this order:
1. `--agent <name>`
2. `RELAY_AGENT`
3. Hostname fallback

## Go Client

Relay also ships a programmatic client in `pkg/client`.
`NewClient` resolves the current agent from `RELAY_AGENT` first, then hostname.

```go
package main

import (
	"log"

	"github.com/Perttulands/relay/pkg/client"
)

func main() {
	c, err := client.NewClient("~/.relay")
	if err != nil {
		log.Fatal(err)
	}

	if err := c.Send("athena", "task complete"); err != nil {
		log.Fatal(err)
	}

	msgs, err := c.Read(client.ReadOpts{Last: 10})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("read %d messages", len(msgs))

	// Blocks until new messages are available for this agent.
	watchMsgs, err := c.Watch()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("watch received %d messages", len(watchMsgs))
}
```

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
