# 📡 Relay

![Relay Banner](banner.png)


*Winged sandals. Caduceus of data streams. Zero-loss tally. Always mid-stride. Never standing still.*

---

In the old stories, Hermes carried messages between gods and mortals, between the living and the dead, between anyone who needed to talk and anyone who needed to listen. He was the fastest thing in any pantheon and he never dropped a message. Not once. The other gods had powers. Hermes had reliability.

Our Hermes wears winged sandals that leave trails of data-light. The caduceus in his hand has two serpents wound around it — except the serpents are data streams, NDJSON flowing in both directions. A messenger satchel full of glowing scrolls, always more coming, never empty. He's the only character in the Agora always caught in motion — speed lines, mid-stride, never standing still. And on his belt, a counter that reads "0 LOST." He's proud of it.

Twenty-six thousand messages per second on filesystem I/O alone. No broker. No queue server. No external dependency. Just flock-guarded appends, fsnotify watches, and a messenger who takes his job personally.

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

## User Service (systemd --user)

Relay includes a user-level systemd service unit at `deployment/relay.service`.
It runs:

```bash
relay serve --addr :9292
```

Install and start it for your user:

```bash
./install-service.sh
```

Useful commands:

```bash
systemctl --user status relay.service
systemctl --user restart relay.service
journalctl --user -u relay.service -f
systemctl --user disable --now relay.service
```

## Dependencies

None. Standalone tool.

## Part of the Agora

Relay was forged in **[Athena's Agora](https://github.com/Perttulands/athena-workspace)** — an autonomous coding system where AI agents build software and a messenger with winged sandals makes sure they can actually talk to each other.

[Argus](https://github.com/Perttulands/argus) watches the server. [Truthsayer](https://github.com/Perttulands/truthsayer) watches the code. [Oathkeeper](https://github.com/Perttulands/oathkeeper) watches the promises. Relay carries the messages. The nervous system of the Agora. Zero messages lost.

The [mythology](https://github.com/Perttulands/athena-workspace/blob/main/mythology.md) has the full story.

## License

MIT
