// Package cli implements the relay command-line interface.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Perttulands/relay/internal/core"
	"github.com/Perttulands/relay/internal/store"
)

const Version = "0.1.0"

var (
	spawnBeadIDRe      = regexp.MustCompile(`Created issue:\s*([^\s]+)`)
	spawnFallbackIDRe  = regexp.MustCompile(`\b([A-Za-z0-9]+-[A-Za-z0-9][A-Za-z0-9-]*)\b`)
	spawnPollInterval  = 2 * time.Second
	spawnPollTimeout   = 30 * time.Minute
	validSpawnAgentSet = map[string]bool{
		"codex":         true,
		"claude:opus":   true,
		"claude:sonnet": true,
		"claude:haiku":  true,
	}
)

// Run is the main entry point. Returns exit code.
func Run(args []string) int {
	if len(args) < 2 {
		usage()
		return 1
	}

	// Parse global flags from the end
	globalFlags, cmdArgs := extractGlobalFlags(args[1:])

	if len(cmdArgs) == 0 {
		usage()
		return 1
	}

	cmd := cmdArgs[0]
	cmdArgs = cmdArgs[1:]

	switch cmd {
	case "version":
		fmt.Println("relay", Version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	}

	// Initialize store
	dir := globalFlags.dir
	if dir == "" {
		dir = os.Getenv("RELAY_DIR")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			errorf("init: resolve home directory: %v", err)
			return 1
		}
		dir = filepath.Join(home, ".relay")
	}
	s, err := store.New(dir)
	if err != nil {
		errorf("init: %v", err)
		return 1
	}

	agent := globalFlags.agent
	if agent == "" {
		agent = os.Getenv("RELAY_AGENT")
	}
	if agent == "" {
		host, err := os.Hostname()
		if err != nil || strings.TrimSpace(host) == "" {
			errorf("init: resolve hostname: %v", err)
			return 1
		}
		agent = host
	}

	ctx := &context{
		store: s,
		agent: agent,
		json:  globalFlags.jsonOut,
		quiet: globalFlags.quiet,
	}

	switch cmd {
	case "register":
		return ctx.cmdRegister(cmdArgs)
	case "heartbeat":
		return ctx.cmdHeartbeat(cmdArgs)
	case "send":
		return ctx.cmdSend(cmdArgs)
	case "read":
		return ctx.cmdRead(cmdArgs)
	case "inbox":
		return ctx.cmdRead(cmdArgs)
	case "status":
		return ctx.cmdStatus(cmdArgs)
	case "watch":
		return ctx.cmdWatch(cmdArgs)
	case "reserve":
		return ctx.cmdReserve(cmdArgs)
	case "release":
		return ctx.cmdRelease(cmdArgs)
	case "reservations":
		return ctx.cmdReservations(cmdArgs)
	case "wake":
		return ctx.cmdWake(cmdArgs)
	case "cmd":
		return ctx.cmdCmd(cmdArgs)
	case "gc":
		return ctx.cmdGC(cmdArgs)
	case "metrics":
		return ctx.cmdMetrics(cmdArgs)
	case "spawn":
		return ctx.cmdSpawn(cmdArgs)
	case "card":
		return ctx.cmdCard(cmdArgs)
	default:
		errorf("unknown command: %s", cmd)
		usage()
		return 1
	}
}

type globalFlagsT struct {
	agent   string
	dir     string
	jsonOut bool
	quiet   bool
}

type context struct {
	store *store.Dir
	agent string
	json  bool
	quiet bool
}

func extractGlobalFlags(args []string) (globalFlagsT, []string) {
	var gf globalFlagsT
	var remaining []string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--agent":
			if i+1 < len(args) {
				gf.agent = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--dir":
			if i+1 < len(args) {
				gf.dir = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--json":
			gf.jsonOut = true
			i++
		case "--quiet":
			gf.quiet = true
			i++
		default:
			remaining = append(remaining, args[i])
			i++
		}
	}
	return gf, remaining
}

func (c *context) cmdRegister(args []string) int {
	if len(args) < 1 {
		errorf("usage: relay register <name> [--program <p>] [--model <m>] [--task <t>] [--bead <b>] [--skills <s1,s2>]")
		return 1
	}
	name := args[0]
	if strings.HasPrefix(name, "-") {
		errorf("register: invalid agent name %q (agent names cannot start with '-')", name)
		return 1
	}
	flags := parseFlags(args[1:])

	now := time.Now().UTC().Format(time.RFC3339)
	meta := core.AgentMeta{
		Name:         name,
		Program:      flags["program"],
		Model:        flags["model"],
		Task:         flags["task"],
		Bead:         flags["bead"],
		RegisteredAt: now,
	}
	if err := c.store.Register(meta); err != nil {
		errorf("register: %v", err)
		return 1
	}

	// Create agent card alongside meta.json.
	card := core.AgentCard{
		Name:         name,
		Status:       core.AgentIdle,
		RegisteredAt: now,
	}
	if skills := flags["skills"]; skills != "" {
		card.Skills = strings.Split(skills, ",")
	}
	if task := flags["task"]; task != "" {
		card.CurrentTask = task
		card.Status = core.AgentWorking
	}
	if err := c.store.WriteCard(card); err != nil {
		errorf("register: write card: %v", err)
		return 1
	}

	if c.json {
		outputJSON(card)
	} else if !c.quiet {
		fmt.Printf("registered agent %s\n", name)
	}
	return 0
}

func (c *context) cmdHeartbeat(args []string) int {
	flags := parseFlags(args)
	idle := flagBool(args, "--idle")

	if err := c.store.Heartbeat(c.agent); err != nil {
		errorf("heartbeat: %v", err)
		return 1
	}
	if task := flags["task"]; task != "" {
		if err := c.store.UpdateTask(c.agent, task); err != nil {
			errorf("update task: %v", err)
			return 1
		}
		// Update card with working status + task.
		card, err := c.store.ReadCard(c.agent)
		if err == nil {
			card.CurrentTask = task
			card.Status = core.AgentWorking
			if writeErr := c.store.WriteCard(card); writeErr != nil {
				errorf("heartbeat: update card: %v", writeErr)
			}
		}
	} else if idle {
		// Clear task and set idle on card.
		card, err := c.store.ReadCard(c.agent)
		if err == nil {
			card.CurrentTask = ""
			card.Status = core.AgentIdle
			if writeErr := c.store.WriteCard(card); writeErr != nil {
				errorf("heartbeat: update card: %v", writeErr)
			}
		}
	}
	if !c.quiet {
		fmt.Println("heartbeat updated")
	}
	return 0
}

func (c *context) cmdSend(args []string) int {
	// Parse: relay send <to> <message> [flags]
	// Or: relay send --broadcast <message> [flags]
	flags := parseFlags(args)
	positional := flagPositional(args)

	broadcast := flagBool(args, "--broadcast")
	wake := flagBool(args, "--wake")

	var to, body string
	if broadcast {
		if len(positional) < 1 {
			errorf("usage: relay send --broadcast <message> [flags]")
			return 1
		}
		body = positional[0]
	} else {
		if len(positional) < 2 {
			errorf("usage: relay send <to> <message> [flags]")
			return 1
		}
		to = positional[0]
		body = positional[1]
	}

	subject := flags["subject"]
	if subject == "" {
		subject = body
		if len(subject) > 80 {
			subject = subject[:80]
		}
	}

	var tags []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--tag") {
			// handled in flags
		}
	}
	if t := flags["tag"]; t != "" {
		tags = strings.Split(t, ",")
	}

	priority := flags["priority"]
	if priority == "" {
		priority = "normal"
	}

	msgType := flags["type"]
	payload := flags["payload"]

	if broadcast {
		agents, err := c.store.ListAgents()
		if err != nil {
			errorf("list agents: %v", err)
			return 1
		}
		count := 0
		for _, name := range agents {
			if name == c.agent {
				continue
			}
			msg := core.Message{
				ID:       core.NewULID(),
				TS:       time.Now().UTC().Format(time.RFC3339),
				From:     c.agent,
				To:       name,
				Subject:  subject,
				Body:     body,
				Thread:   flags["thread"],
				Priority: priority,
				Tags:     tags,
			}
			if msgType != "" {
				msg.Type = msgType
			}
			if payload != "" {
				msg.Payload = json.RawMessage(payload)
			}
			if err := c.store.Send(msg); err != nil {
				errorf("send to %s: %v", name, err)
			} else {
				count++
			}
		}
		if !c.quiet {
			fmt.Printf("broadcast to %d agents\n", count)
		}
	} else {
		msg := core.Message{
			ID:       core.NewULID(),
			TS:       time.Now().UTC().Format(time.RFC3339),
			From:     c.agent,
			To:       to,
			Subject:  subject,
			Body:     body,
			Thread:   flags["thread"],
			Priority: priority,
			Tags:     tags,
		}
		if msgType != "" {
			msg.Type = msgType
		}
		if payload != "" {
			msg.Payload = json.RawMessage(payload)
		}
		if err := c.store.Send(msg); err != nil {
			errorf("send: %v", err)
			return 1
		}
		if c.json {
			outputJSON(msg)
		} else if !c.quiet {
			fmt.Printf("sent message to %s (id: %s)\n", to, msg.ID)
		}
	}

	if wake {
		// Try to wake the target agent's OpenClaw service first
		if to != "" {
			svcName := fmt.Sprintf("openclaw-%s.service", to)
			cmd := execCommand("systemctl", "--user", "start", svcName)
			if err := cmd.Run(); err == nil {
				if !c.quiet {
					fmt.Printf("wake: started %s\n", svcName)
				}
			} else {
				// Fall back to default wake (Athena gateway)
				c.doWake("")
			}
		} else {
			c.doWake("")
		}
	}

	return 0
}

func (c *context) cmdRead(args []string) int {
	flags := parseFlags(args)
	opts := store.ReadOpts{
		From:     flags["from"],
		Thread:   flags["thread"],
		Type:     flags["type"],
		Unread:   flagBool(args, "--unread"),
		MarkRead: flagBool(args, "--mark-read"),
	}

	if s := flags["since"]; s != "" {
		opts.Since = parseSince(s)
	}
	if n := flags["last"]; n != "" {
		fmt.Sscanf(n, "%d", &opts.Last)
	}
	if opts.Last == 0 && !opts.Unread {
		opts.Last = 20
	}

	msgs, err := c.store.ReadInbox(c.agent, opts)
	if err != nil {
		errorf("read: %v", err)
		return 1
	}

	if c.json {
		outputJSON(msgs)
	} else {
		if len(msgs) == 0 {
			if !c.quiet {
				fmt.Println("no messages")
			}
		} else {
			for _, m := range msgs {
				age := "now"
				ts, err := time.Parse(time.RFC3339, m.TS)
				if err != nil {
					errorf("read: invalid message timestamp for %s: %v", m.ID, err)
				} else {
					age = formatAge(time.Since(ts))
				}
				prio := ""
				if m.Priority != "" && m.Priority != "normal" {
					prio = fmt.Sprintf(" [%s]", strings.ToUpper(m.Priority))
				}
				fmt.Printf("  %s  %-16s %s%s\n", age, m.From, m.Subject, prio)
				if m.Body != m.Subject && m.Body != "" {
					// Show body if different from subject
					body := m.Body
					if len(body) > 200 {
						body = body[:200] + "..."
					}
					fmt.Printf("    %s\n", body)
				}
			}
			fmt.Printf("\n%d message(s)\n", len(msgs))
		}
	}
	return 0
}

func (c *context) cmdStatus(args []string) int {
	flags := parseFlags(args)
	staleStr := flags["stale"]
	staleThreshold := 5 * time.Minute
	if staleStr != "" {
		staleThreshold = parseDuration(staleStr)
	}

	agents, err := c.store.ListAgents()
	if err != nil {
		errorf("status: list agents: %v", err)
		return 1
	}
	reservations, err := c.store.ListReservations()
	if err != nil {
		errorf("status: list reservations: %v", err)
		return 1
	}
	commands, err := c.store.ListCommands()
	if err != nil {
		errorf("status: list commands: %v", err)
		return 1
	}

	if c.json {
		type statusJSON struct {
			Agents       []core.AgentStatus `json:"agents"`
			Reservations []core.Reservation `json:"reservations"`
			Commands     []core.Command     `json:"commands"`
		}
		var agentStatuses []core.AgentStatus
		for _, name := range agents {
			hb, err := c.store.ReadHeartbeatTime(name)
			card, cardErr := c.store.ReadCard(name)
			meta, metaErr := c.store.ReadMeta(name)
			status := core.AgentStatus{
				Name:  name,
				Alive: err == nil && time.Since(hb) < staleThreshold,
			}
			// Prefer card data over meta for task/skills/status.
			if cardErr == nil {
				status.Task = card.CurrentTask
				status.Skills = card.Skills
				status.CardStatus = card.Status
			} else if metaErr == nil {
				status.Task = meta.Task
			}
			if err == nil {
				status.LastHeartbeat = hb
			}
			agentStatuses = append(agentStatuses, status)
		}
		outputJSON(statusJSON{
			Agents:       agentStatuses,
			Reservations: reservations,
			Commands:     commands,
		})
		return 0
	}

	// Text output
	aliveCount, staleCount := 0, 0
	fmt.Println("AGENTS")
	for _, name := range agents {
		hb, err := c.store.ReadHeartbeatTime(name)
		card, cardErr := c.store.ReadCard(name)
		meta, metaErr := c.store.ReadMeta(name)
		task := ""
		cardStatus := ""
		skills := ""
		if cardErr == nil {
			task = card.CurrentTask
			cardStatus = card.Status
			if len(card.Skills) > 0 {
				skills = strings.Join(card.Skills, ",")
			}
		} else if metaErr == nil {
			task = meta.Task
		} else {
			errorf("status: read meta for %s: %v", name, metaErr)
		}
		if err != nil {
			staleCount++
			fmt.Printf("  %-20s STALE   heartbeat: missing      task: %s\n", name, task)
			continue
		}
		age := time.Since(hb)
		extra := ""
		if cardStatus != "" {
			extra += fmt.Sprintf(" status: %s", cardStatus)
		}
		if skills != "" {
			extra += fmt.Sprintf(" skills: [%s]", skills)
		}
		if age > staleThreshold {
			staleCount++
			fmt.Printf("  %-20s STALE   last heartbeat: %-8s task: %s%s\n", name, formatAge(age), task, extra)
		} else {
			aliveCount++
			fmt.Printf("  %-20s alive   last heartbeat: %-8s task: %s%s\n", name, formatAge(age), task, extra)
		}
	}
	fmt.Printf("  (%d alive, %d stale)\n", aliveCount, staleCount)

	fmt.Println("\nRESERVATIONS")
	activeRes, expiredRes := 0, 0
	now := time.Now()
	for _, r := range reservations {
		expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
		if err != nil {
			errorf("status: invalid reservation expiry for %s: %v", r.Pattern, err)
		}
		excl := "exclusive"
		if !r.Exclusive {
			excl = "shared"
		}
		if err != nil || now.After(expires) {
			expiredRes++
			if err != nil {
				fmt.Printf("  %-25s %-16s %-10s EXPIRED invalid-ts %s\n",
					r.Pattern, r.Agent, excl, filepath.Base(r.Repo))
			} else {
				fmt.Printf("  %-25s %-16s %-10s EXPIRED %s ago    %s\n",
					r.Pattern, r.Agent, excl, formatAge(now.Sub(expires)), filepath.Base(r.Repo))
			}
		} else {
			activeRes++
			fmt.Printf("  %-25s %-16s %-10s expires in %-8s %s\n",
				r.Pattern, r.Agent, excl, formatAge(expires.Sub(now)), filepath.Base(r.Repo))
		}
	}
	if len(reservations) == 0 {
		fmt.Println("  (none)")
	} else {
		fmt.Printf("  (%d active, %d expired)\n", activeRes, expiredRes)
	}

	pendingCmds := 0
	for _, cmd := range commands {
		if cmd.Status == "pending" {
			pendingCmds++
		}
	}
	if pendingCmds > 0 {
		fmt.Printf("\nPENDING COMMANDS (%d)\n", pendingCmds)
		for _, cmd := range commands {
			if cmd.Status != "pending" {
				continue
			}
			age := "unknown"
			ts, err := time.Parse(time.RFC3339, cmd.TS)
			if err != nil {
				errorf("status: invalid command timestamp for %s: %v", cmd.ID, err)
			} else {
				age = formatAge(time.Since(ts))
			}
			fmt.Printf("  %-16s %s → %s   %s %s   %s ago\n",
				cmd.ID[:16], cmd.From, cmd.TargetSession, cmd.Command, cmd.Args, age)
		}
	}

	return 0
}

func (c *context) cmdWatch(args []string) int {
	loop := flagBool(args, "--loop")
	var offset int64

	for {
		msgs, newOffset, err := c.store.WatchInbox(c.agent, offset)
		if err != nil {
			errorf("watch: %v", err)
			return 1
		}
		offset = newOffset

		for _, m := range msgs {
			if c.json {
				outputJSON(m)
				continue
			}
			ts, err := time.Parse(time.RFC3339, m.TS)
			if err != nil {
				errorf("watch: invalid message timestamp for %s: %v", m.ID, err)
				fmt.Printf("%s  %-16s %s\n", "now", m.From, m.Subject)
			} else {
				fmt.Printf("%s  %-16s %s\n", formatAge(time.Since(ts)), m.From, m.Subject)
			}
			if m.Body != m.Subject && m.Body != "" {
				fmt.Printf("    %s\n", m.Body)
			}
		}

		if !loop {
			return 0
		}
	}
}

func (c *context) cmdReserve(args []string) int {
	if len(args) < 1 {
		errorf("usage: relay reserve <pattern> [flags]")
		return 1
	}

	positional := flagPositional(args)
	if len(positional) < 1 {
		errorf("usage: relay reserve <pattern> [flags]")
		return 1
	}
	pattern := positional[0]
	flags := parseFlags(args)

	repo := flags["repo"]
	if repo == "" {
		var err error
		repo, err = os.Getwd()
		if err != nil {
			errorf("reserve: getwd: %v", err)
			return 1
		}
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		errorf("reserve: absolute repo path: %v", err)
		return 1
	}

	ttl := parseDuration(flags["ttl"])
	if ttl == 0 {
		ttl = time.Hour
	}

	check := flagBool(args, "--check")
	force := flagBool(args, "--force")
	shared := flagBool(args, "--shared")

	// Check for overlaps
	conflicts, err := c.store.CheckOverlap(c.agent, repo, pattern)
	if err != nil {
		errorf("check overlap: %v", err)
		return 1
	}
	if len(conflicts) > 0 {
		for _, r := range conflicts {
			fmt.Printf("  conflict: %s reserved by %s (%s)\n", r.Pattern, r.Agent, r.Reason)
		}
		if check {
			return 1
		}
		if !force {
			errorf("conflicts found (use --force to override)")
			return 1
		}
	}

	if check {
		if !c.quiet {
			fmt.Println("no conflicts")
		}
		return 0
	}

	res := core.Reservation{
		ID:        core.NewULID(),
		Agent:     c.agent,
		Pattern:   pattern,
		Repo:      repo,
		Exclusive: !shared,
		Reason:    flags["reason"],
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(ttl).UTC().Format(time.RFC3339),
	}

	if err := c.store.Reserve(res); err != nil {
		if force && strings.Contains(err.Error(), "conflict") {
			// Force: remove existing and retry
			if releaseErr := c.store.Release(c.agent, repo, pattern); releaseErr != nil {
				errorf("reserve (force): release existing reservation: %v", releaseErr)
			}
			// Also try removing other agent's reservation
			hash := store.ReservationHash(repo, pattern)
			if removeErr := os.Remove(filepath.Join(c.store.Root, "reservations", hash+".json")); removeErr != nil && !os.IsNotExist(removeErr) {
				errorf("reserve (force): remove conflicting reservation file: %v", removeErr)
				return 1
			}
			if err := c.store.Reserve(res); err != nil {
				errorf("reserve (force): %v", err)
				return 1
			}
		} else {
			errorf("reserve: %v", err)
			return 1
		}
	}

	if c.json {
		outputJSON(res)
	} else if !c.quiet {
		fmt.Printf("reserved %s (expires in %s)\n", pattern, formatAge(ttl))
	}
	return 0
}

func (c *context) cmdRelease(args []string) int {
	if flagBool(args, "--all") {
		count, err := c.store.ReleaseAll(c.agent)
		if err != nil {
			errorf("release all: %v", err)
			return 1
		}
		if !c.quiet {
			fmt.Printf("released %d reservation(s)\n", count)
		}
		return 0
	}

	positional := flagPositional(args)
	if len(positional) < 1 {
		errorf("usage: relay release <pattern> [--repo <path>] | relay release --all")
		return 1
	}
	pattern := positional[0]
	flags := parseFlags(args)

	repo := flags["repo"]
	if repo == "" {
		var err error
		repo, err = os.Getwd()
		if err != nil {
			errorf("release: getwd: %v", err)
			return 1
		}
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		errorf("release: absolute repo path: %v", err)
		return 1
	}

	if err := c.store.Release(c.agent, repo, pattern); err != nil {
		errorf("release: %v", err)
		return 1
	}
	if !c.quiet {
		fmt.Printf("released %s\n", pattern)
	}
	return 0
}

func (c *context) cmdReservations(args []string) int {
	flags := parseFlags(args)
	reservations, err := c.store.ListReservations()
	if err != nil {
		errorf("list reservations: %v", err)
		return 1
	}

	repoFilter := flags["repo"]
	if repoFilter != "" {
		var err error
		repoFilter, err = filepath.Abs(repoFilter)
		if err != nil {
			errorf("reservations: absolute repo path: %v", err)
			return 1
		}
	}
	agentFilter := flags["agent"]
	showExpired := flagBool(args, "--expired")
	now := time.Now()

	var filtered []core.Reservation
	for _, r := range reservations {
		if repoFilter != "" && r.Repo != repoFilter {
			continue
		}
		if agentFilter != "" && r.Agent != agentFilter {
			continue
		}
		expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
		if err != nil {
			errorf("reservations: invalid expiry for %s: %v", r.Pattern, err)
			if showExpired {
				filtered = append(filtered, r)
			}
			continue
		}
		if !showExpired && now.After(expires) {
			continue
		}
		filtered = append(filtered, r)
	}

	if c.json {
		outputJSON(filtered)
		return 0
	}

	if len(filtered) == 0 {
		if !c.quiet {
			fmt.Println("no reservations")
		}
		return 0
	}

	for _, r := range filtered {
		expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
		if err != nil {
			errorf("reservations: invalid expiry for %s: %v", r.Pattern, err)
		}
		excl := "exclusive"
		if !r.Exclusive {
			excl = "shared"
		}
		if err != nil || now.After(expires) {
			if err != nil {
				fmt.Printf("  %-25s %-16s %-10s EXPIRED invalid-ts %s\n",
					r.Pattern, r.Agent, excl, filepath.Base(r.Repo))
			} else {
				fmt.Printf("  %-25s %-16s %-10s EXPIRED %s ago    %s\n",
					r.Pattern, r.Agent, excl, formatAge(now.Sub(expires)), filepath.Base(r.Repo))
			}
		} else {
			fmt.Printf("  %-25s %-16s %-10s expires in %-8s %s\n",
				r.Pattern, r.Agent, excl, formatAge(expires.Sub(now)), filepath.Base(r.Repo))
		}
	}
	return 0
}

func (c *context) cmdWake(args []string) int {
	positional := flagPositional(args)
	text := strings.Join(positional, " ")

	flags := parseFlags(args)
	method := flags["method"]

	return c.doWakeMethod(text, method)
}

func (c *context) doWake(text string) int {
	return c.doWakeMethod(text, "")
}

func (c *context) doWakeMethod(text, method string) int {
	if method != "" {
		switch method {
		case "gateway":
			if err := wakeGateway(text); err != nil {
				errorf("wake (gateway): %v", err)
				return 1
			}
			if !c.quiet {
				fmt.Println("wake: gateway")
			}
			return 0
		case "file":
			if err := c.store.TouchWake(text); err != nil {
				errorf("wake (file): %v", err)
				return 1
			}
			if !c.quiet {
				fmt.Println("wake: file trigger")
			}
			return 0
		default:
			errorf("unknown wake method: %s", method)
			return 1
		}
	}

	// Auto: try gateway first, then file trigger
	if err := wakeGateway(text); err == nil {
		if !c.quiet {
			fmt.Println("wake: gateway")
		}
		return 0
	}

	// Fallback: file trigger
	if err := c.store.TouchWake(text); err != nil {
		errorf("wake (all methods failed): %v", err)
		return 1
	}
	if !c.quiet {
		fmt.Println("wake: file trigger")
	}
	return 0
}

func wakeGateway(text string) error {
	// Try wake-gateway.sh
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return fmt.Errorf("resolve home directory for wake gateway")
	}
	path := filepath.Join(home, ".openclaw", "workspace", "scripts", "wake-gateway.sh")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("wake-gateway.sh not found")
	}
	cmd := execCommand(path, text)
	return cmd.Run()
}

func (c *context) cmdCmd(args []string) int {
	if len(args) < 2 {
		errorf("usage: relay cmd <target-session> <command> [args...]")
		return 1
	}

	target := args[0]
	command := args[1]
	cmdArgs := ""
	if len(args) > 2 {
		// Collect remaining positional args
		remaining := flagPositional(args[2:])
		cmdArgs = strings.Join(remaining, " ")
	}

	wake := flagBool(args, "--wake")

	cmd := core.Command{
		ID:            core.NewULID(),
		TS:            time.Now().UTC().Format(time.RFC3339),
		From:          c.agent,
		TargetSession: target,
		Command:       command,
		Args:          cmdArgs,
		Status:        "pending",
	}
	if err := c.store.CreateCommand(cmd); err != nil {
		errorf("cmd: %v", err)
		return 1
	}

	if c.json {
		outputJSON(cmd)
	} else if !c.quiet {
		fmt.Printf("command posted: %s %s → %s (id: %s)\n", command, cmdArgs, target, cmd.ID)
	}

	if wake {
		c.doWake(fmt.Sprintf("command: %s %s", command, cmdArgs))
	}
	return 0
}

func (c *context) cmdGC(args []string) int {
	flags := parseFlags(args)
	dryRun := flagBool(args, "--dry-run")
	expiredOnly := flagBool(args, "--expired-only")

	staleThreshold := 30 * time.Minute
	if s := flags["stale"]; s != "" {
		staleThreshold = parseDuration(s)
	}

	if dryRun {
		// Show what would be cleaned
		reservations, err := c.store.ListReservations()
		if err != nil {
			errorf("gc: list reservations: %v", err)
			return 1
		}
		now := time.Now()
		expCount := 0
		for _, r := range reservations {
			expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
			if err != nil {
				errorf("gc: invalid reservation expiry for %s: %v", r.Pattern, err)
				expCount++
				fmt.Printf("  would remove: reservation %s (%s) [invalid expiry]\n", r.Pattern, r.Agent)
				continue
			}
			if now.After(expires) {
				expCount++
				fmt.Printf("  would remove: reservation %s (%s)\n", r.Pattern, r.Agent)
			}
		}
		fmt.Printf("dry run: %d expired reservations\n", expCount)
		return 0
	}

	result, err := c.store.GC(staleThreshold, expiredOnly)
	if err != nil {
		errorf("gc: %v", err)
		return 1
	}

	if c.json {
		outputJSON(result)
	} else if !c.quiet {
		fmt.Printf("gc: removed %d expired reservations, %d old commands, %d stale agents\n",
			result.ExpiredReservations, result.OldCommands, result.StaleAgents)
	}
	return 0
}

func (c *context) cmdMetrics(args []string) int {
	flags := parseFlags(args)
	staleThreshold := 5 * time.Minute
	if s := flags["stale"]; s != "" {
		staleThreshold = parseDuration(s)
	}

	m, err := c.store.Metrics(staleThreshold)
	if err != nil {
		errorf("metrics: %v", err)
		return 1
	}

	if c.json {
		outputJSON(m)
		return 0
	}

	fmt.Printf("AGENTS          %d total (%d alive, %d stale)\n", m.Agents, m.AliveAgents, m.StaleAgents)
	fmt.Printf("MESSAGES        %d total\n", m.TotalMessages)
	fmt.Printf("RESERVATIONS    %d total (%d active, %d expired)\n", m.Reservations, m.ActiveReservations, m.ExpiredReservations)
	fmt.Printf("COMMANDS        %d total (%d pending)\n", m.Commands, m.PendingCommands)
	return 0
}

func (c *context) cmdCard(args []string) int {
	positional := flagPositional(args)
	listAll := flagBool(args, "--all")

	if listAll {
		cards, err := c.store.ListCards()
		if err != nil {
			errorf("card: list cards: %v", err)
			return 1
		}
		if c.json {
			outputJSON(cards)
		} else {
			if len(cards) == 0 {
				if !c.quiet {
					fmt.Println("no agent cards")
				}
			} else {
				for _, card := range cards {
					printCard(card)
				}
			}
		}
		return 0
	}

	// Determine target agent: positional arg or self.
	target := c.agent
	if len(positional) > 0 {
		target = positional[0]
	}

	card, err := c.store.ReadCard(target)
	if err != nil {
		errorf("card: %v", err)
		return 1
	}

	if c.json {
		outputJSON(card)
	} else {
		printCard(card)
	}
	return 0
}

func printCard(card core.AgentCard) {
	skills := "(none)"
	if len(card.Skills) > 0 {
		skills = strings.Join(card.Skills, ", ")
	}
	status := card.Status
	if status == "" {
		status = "unknown"
	}
	task := card.CurrentTask
	if task == "" {
		task = "(none)"
	}
	fmt.Printf("  %-20s status: %-10s task: %-20s skills: %s\n", card.Name, status, task, skills)
}

func (c *context) cmdSpawn(args []string) int {
	flags := parseFlags(args)
	repo := strings.TrimSpace(flags["repo"])
	agentType := strings.TrimSpace(flags["agent"])
	if agentType == "" && validSpawnAgentSet[c.agent] {
		agentType = c.agent
	}
	prompt := strings.TrimSpace(flags["prompt"])
	title := strings.TrimSpace(flags["title"])
	wait := flagBool(args, "--wait")
	notify := strings.TrimSpace(flags["notify"])

	if repo == "" || agentType == "" || prompt == "" {
		errorf("usage: relay spawn --repo <path> --agent <type> --prompt <text> [--title <text>] [--wait] [--notify <agent>]")
		return 1
	}
	if !validSpawnAgentSet[agentType] {
		errorf("spawn: invalid --agent %q (expected codex|claude:opus|claude:sonnet|claude:haiku)", agentType)
		return 1
	}
	if title == "" {
		title = prompt
		r := []rune(title)
		if len(r) > 50 {
			title = string(r[:50])
		}
	}

	brBin := resolveBRBinary()
	createCmd := execCommand(brBin, "create", title, "-t", "task")
	// Run br in workspace (where beads DB lives), not target repo
	workspaceDir := resolveWorkspaceDir()
	createCmd.Dir = workspaceDir
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		errorf("spawn: br create failed: %v (%s)", err, strings.TrimSpace(string(createOut)))
		return 1
	}

	beadID := extractSpawnBeadID(string(createOut))
	if beadID == "" {
		errorf("spawn: could not parse bead id from br output: %s", strings.TrimSpace(string(createOut)))
		return 1
	}

	fmt.Printf("spawned %s\n", beadID)

	dispatchScript, err := resolveDispatchScript()
	if err != nil {
		errorf("spawn: %v", err)
		return 1
	}

	dispatchCmd := execCommand(dispatchScript, beadID, repo, agentType, prompt)
	dispatchCmd.Env = append(os.Environ(), "DISPATCH_ENFORCE_PRD_LINT=false")
	dispatchOut, err := dispatchCmd.CombinedOutput()
	if err != nil {
		errorf("spawn: dispatch failed: %v (%s)", err, strings.TrimSpace(string(dispatchOut)))
		return 1
	}

	if wait {
		result, waitErr := waitForSpawnResult(repo, beadID)
		if waitErr != nil {
			errorf("spawn: wait failed: %v", waitErr)
			return 1
		}
		if strings.TrimSpace(result) != "" {
			fmt.Println(result)
		}
		if notify != "" {
			msg := fmt.Sprintf("Spawned task %s completed", beadID)
			notifyCmd := execCommand("relay", "send", notify, msg)
			notifyOut, notifyErr := notifyCmd.CombinedOutput()
			if notifyErr != nil {
				errorf("spawn: notify failed: %v (%s)", notifyErr, strings.TrimSpace(string(notifyOut)))
				return 1
			}
		}
	}

	return 0
}

func resolveBRBinary() string {
	home, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home, ".cargo", "bin", "br")
		if st, statErr := os.Stat(candidate); statErr == nil && !st.IsDir() {
			return candidate
		}
	}
	return "br"
}

func extractSpawnBeadID(output string) string {
	if m := spawnBeadIDRe.FindStringSubmatch(output); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := spawnFallbackIDRe.FindStringSubmatch(output); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func resolveDispatchScript() (string, error) {
	if fromEnv := strings.TrimSpace(os.Getenv("DISPATCH_SCRIPT")); fromEnv != "" {
		if st, err := os.Stat(fromEnv); err == nil && !st.IsDir() {
			return fromEnv, nil
		}
		return "", fmt.Errorf("dispatch script not found at DISPATCH_SCRIPT=%s", fromEnv)
	}

	var candidates []string

	home, err := os.UserHomeDir()
	if err == nil {
		candidates = append(candidates, filepath.Join(home, "athena", "workspace", "scripts", "dispatch.sh"))
	}

	for _, p := range candidates {
		if st, statErr := os.Stat(p); statErr == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("dispatch script not found (set DISPATCH_SCRIPT)")
}

func resolveWorkspaceDir() string {
	if ws := strings.TrimSpace(os.Getenv("ATHENA_WORKSPACE")); ws != "" {
		return ws
	}
	home, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home, "athena", "workspace")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		// Directory doesn't exist yet; return the path anyway as fallback.
		return candidate
	}
	return filepath.Join("athena", "workspace")
}

func waitForSpawnResult(repo, beadID string) (string, error) {
	resultPath := filepath.Join(repo, "state", "results", beadID+".json")
	deadline := time.Now().Add(spawnPollTimeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(resultPath)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		time.Sleep(spawnPollInterval)
	}
	return "", fmt.Errorf("timed out waiting for result: %s", resultPath)
}

// Helper functions

func usage() {
	fmt.Print(`relay — Agent-to-agent communication

COMMANDS:
  relay send <to> <message>           Send a message to an agent's inbox
  relay read [flags]                  Read messages from your inbox
  relay inbox [flags]                 Alias for read
  relay watch [--loop]                Block until new inbox message(s) arrive
  relay reserve <pattern> [flags]     Reserve file paths
  relay release <pattern>             Release a file reservation
  relay reservations [flags]          List active reservations
  relay wake [text]                   Wake Athena (OpenClaw gateway)
  relay cmd <session> <command>       Inject a slash command into a session
  relay spawn [flags]                 Spawn an agent task via dispatch
  relay status                        Show all agents, heartbeats, reservations
  relay register <name> [flags]       Register agent identity
  relay heartbeat                     Update agent heartbeat
  relay card [agent]                   Show an agent's card (default: self)
  relay card --all                    Show all agent cards
  relay metrics [flags]               Show aggregate system metrics
  relay gc                            Clean up expired reservations and stale agents
  relay version                       Print version

SEND FLAGS:
  --subject <text>   Message subject (default: first 80 chars of body)
  --thread <id>      Thread identifier
  --priority <p>     Priority level (default: normal)
  --tag <t1,t2>      Comma-separated tags
  --type <type>      Message type: task_result, request, alert, status, chat
  --payload <json>   Structured JSON payload (type-specific)
  --broadcast        Send to all registered agents
  --wake             Wake target agent after sending

READ FLAGS:
  --from <agent>     Filter by sender
  --thread <id>      Filter by thread
  --type <type>      Filter by message type
  --since <time>     Filter messages after time (duration, RFC3339, or date)
  --last <n>         Show last N messages (default: 20)
  --unread           Show only unread messages
  --mark-read        Mark returned messages as read

GLOBAL FLAGS:
  --agent <name>     Agent identity (default: $RELAY_AGENT or hostname)
  --dir <path>       Data directory (default: ~/.relay)
  --json             Output as JSON (for scripting)
  --quiet            Suppress non-essential output

NOTES:
  Commands that act as "you" (send/read/inbox/watch/heartbeat/reserve/release/cmd)
  use --agent, then RELAY_AGENT, then hostname.
`)
}

func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "relay: "+format+"\n", args...)
}

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// parseFlags extracts --key value pairs from args.
func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			key := strings.TrimPrefix(args[i], "--")
			// Skip boolean flags
			if key == "broadcast" || key == "wake" || key == "check" || key == "force" ||
				key == "shared" || key == "all" || key == "unread" || key == "mark-read" ||
				key == "dry-run" || key == "expired-only" || key == "expired" || key == "tail" ||
				key == "loop" || key == "wait" || key == "idle" {
				continue
			}
			flags[key] = args[i+1]
			i++
		}
	}
	return flags
}

// flagBool checks if a boolean flag is present.
func flagBool(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// flagPositional extracts non-flag arguments.
func flagPositional(args []string) []string {
	var pos []string
	boolFlags := map[string]bool{
		"--broadcast": true, "--wake": true, "--check": true, "--force": true,
		"--shared": true, "--all": true, "--unread": true, "--mark-read": true,
		"--dry-run": true, "--expired-only": true, "--expired": true, "--tail": true,
		"--json": true, "--quiet": true, "--loop": true, "--wait": true, "--idle": true,
	}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			if boolFlags[args[i]] {
				continue
			}
			i++ // skip value
			continue
		}
		pos = append(pos, args[i])
	}
	return pos
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	// Try Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	// Try simple suffixes
	if strings.HasSuffix(s, "s") {
		var n int
		fmt.Sscanf(s, "%ds", &n)
		return time.Duration(n) * time.Second
	}
	if strings.HasSuffix(s, "m") {
		var n int
		fmt.Sscanf(s, "%dm", &n)
		return time.Duration(n) * time.Minute
	}
	if strings.HasSuffix(s, "h") {
		var n int
		fmt.Sscanf(s, "%dh", &n)
		return time.Duration(n) * time.Hour
	}
	return 0
}

func parseSince(s string) time.Time {
	// Try as duration
	if d := parseDuration(s); d > 0 {
		return time.Now().Add(-d)
	}
	// Try as RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try as date
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
