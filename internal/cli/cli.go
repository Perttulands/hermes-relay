// Package cli implements the relay command-line interface.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Perttulands/relay/internal/core"
	"github.com/Perttulands/relay/internal/store"
)

const Version = "0.1.0"

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
		home, _ := os.UserHomeDir()
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
		agent, _ = os.Hostname()
	}

	ctx := &context{
		store:  s,
		agent:  agent,
		json:   globalFlags.jsonOut,
		quiet:  globalFlags.quiet,
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
		errorf("usage: relay register <name> [--program <p>] [--model <m>] [--task <t>] [--bead <b>]")
		return 1
	}
	name := args[0]
	if strings.HasPrefix(name, "-") {
		errorf("register: invalid agent name %q (agent names cannot start with '-')", name)
		return 1
	}
	flags := parseFlags(args[1:])

	meta := core.AgentMeta{
		Name:         name,
		Program:      flags["program"],
		Model:        flags["model"],
		Task:         flags["task"],
		Bead:         flags["bead"],
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := c.store.Register(meta); err != nil {
		errorf("register: %v", err)
		return 1
	}

	if c.json {
		outputJSON(meta)
	} else if !c.quiet {
		fmt.Printf("registered agent %s\n", name)
	}
	return 0
}

func (c *context) cmdHeartbeat(args []string) int {
	flags := parseFlags(args)
	if err := c.store.Heartbeat(c.agent); err != nil {
		errorf("heartbeat: %v", err)
		return 1
	}
	if task := flags["task"]; task != "" {
		if err := c.store.UpdateTask(c.agent, task); err != nil {
			errorf("update task: %v", err)
			return 1
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
		c.doWake("")
	}

	return 0
}

func (c *context) cmdRead(args []string) int {
	flags := parseFlags(args)
	opts := store.ReadOpts{
		From:     flags["from"],
		Thread:   flags["thread"],
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
				ts, _ := time.Parse(time.RFC3339, m.TS)
				age := formatAge(time.Since(ts))
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

	agents, _ := c.store.ListAgents()
	reservations, _ := c.store.ListReservations()
	commands, _ := c.store.ListCommands()

	if c.json {
		type statusJSON struct {
			Agents       []core.AgentStatus  `json:"agents"`
			Reservations []core.Reservation  `json:"reservations"`
			Commands     []core.Command      `json:"commands"`
		}
		var agentStatuses []core.AgentStatus
		for _, name := range agents {
			hb, err := c.store.ReadHeartbeat(name)
			meta, _ := c.store.ReadMeta(name)
			status := core.AgentStatus{
				Name:  name,
				Task:  meta.Task,
				Alive: err == nil && time.Since(hb) < staleThreshold,
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
		hb, err := c.store.ReadHeartbeat(name)
		meta, _ := c.store.ReadMeta(name)
		if err != nil {
			staleCount++
			fmt.Printf("  %-20s STALE   heartbeat: missing      task: %s\n", name, meta.Task)
			continue
		}
		age := time.Since(hb)
		if age > staleThreshold {
			staleCount++
			fmt.Printf("  %-20s STALE   last heartbeat: %-8s task: %s\n", name, formatAge(age), meta.Task)
		} else {
			aliveCount++
			fmt.Printf("  %-20s alive   last heartbeat: %-8s task: %s\n", name, formatAge(age), meta.Task)
		}
	}
	fmt.Printf("  (%d alive, %d stale)\n", aliveCount, staleCount)

	fmt.Println("\nRESERVATIONS")
	activeRes, expiredRes := 0, 0
	now := time.Now()
	for _, r := range reservations {
		expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
		excl := "exclusive"
		if !r.Exclusive {
			excl = "shared"
		}
		if now.After(expires) {
			expiredRes++
			fmt.Printf("  %-25s %-16s %-10s EXPIRED %s ago    %s\n",
				r.Pattern, r.Agent, excl, formatAge(now.Sub(expires)), filepath.Base(r.Repo))
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
			ts, _ := time.Parse(time.RFC3339, cmd.TS)
			fmt.Printf("  %-16s %s → %s   %s %s   %s ago\n",
				cmd.ID[:16], cmd.From, cmd.TargetSession, cmd.Command, cmd.Args, formatAge(time.Since(ts)))
		}
	}

	return 0
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
		repo, _ = os.Getwd()
	}
	repo, _ = filepath.Abs(repo)

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
			c.store.Release(c.agent, repo, pattern)
			// Also try removing other agent's reservation
			hash := store.ReservationHash(repo, pattern)
			os.Remove(filepath.Join(c.store.Root, "reservations", hash+".json"))
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
		repo, _ = os.Getwd()
	}
	repo, _ = filepath.Abs(repo)

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
		repoFilter, _ = filepath.Abs(repoFilter)
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
		expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
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
		expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
		excl := "exclusive"
		if !r.Exclusive {
			excl = "shared"
		}
		if now.After(expires) {
			fmt.Printf("  %-25s %-16s %-10s EXPIRED %s ago    %s\n",
				r.Pattern, r.Agent, excl, formatAge(now.Sub(expires)), filepath.Base(r.Repo))
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
	path := filepath.Join(os.Getenv("HOME"), ".openclaw", "workspace", "scripts", "wake-gateway.sh")
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
		reservations, _ := c.store.ListReservations()
		now := time.Now()
		expCount := 0
		for _, r := range reservations {
			expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
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

// Helper functions

func usage() {
	fmt.Print(`relay — Agent-to-agent communication

COMMANDS:
  relay send <to> <message>           Send a message to an agent's inbox
  relay read [flags]                  Read messages from your inbox
  relay inbox [flags]                 Alias for read
  relay reserve <pattern> [flags]     Reserve file paths
  relay release <pattern>             Release a file reservation
  relay reservations [flags]          List active reservations
  relay wake [text]                   Wake Athena (OpenClaw gateway)
  relay cmd <session> <command>       Inject a slash command into a session
  relay status                        Show all agents, heartbeats, reservations
  relay register <name> [flags]       Register agent identity
  relay heartbeat                     Update agent heartbeat
  relay gc                            Clean up expired reservations and stale agents
  relay version                       Print version

GLOBAL FLAGS:
  --agent <name>     Agent identity (default: $RELAY_AGENT or hostname)
  --dir <path>       Data directory (default: ~/.relay)
  --json             Output as JSON (for scripting)
  --quiet            Suppress non-essential output
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
				key == "dry-run" || key == "expired-only" || key == "expired" || key == "tail" {
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
		"--json": true, "--quiet": true,
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
