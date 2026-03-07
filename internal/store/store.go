// Package store implements filesystem operations for relay.
// All writes use atomic rename or flock-guarded append.
package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
	"github.com/fsnotify/fsnotify"
)

// Dir is the relay data directory (default ~/.relay).
type Dir struct {
	Root string
}

// New creates a Dir and ensures all subdirectories exist.
func New(root string) (*Dir, error) {
	dirs := []string{
		filepath.Join(root, "agents"),
		filepath.Join(root, "reservations"),
		filepath.Join(root, "commands"),
		filepath.Join(root, "wake"),
		filepath.Join(root, "chains"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return &Dir{Root: root}, nil
}

// AgentDir returns the path to an agent's directory.
func (d *Dir) AgentDir(name string) string {
	return filepath.Join(d.Root, "agents", name)
}

// Register creates or updates an agent's meta.json and writes an initial heartbeat.
func (d *Dir) Register(meta core.AgentMeta) error {
	dir := d.AgentDir(meta.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create agent dir %s: %w", meta.Name, err)
	}
	if err := atomicWriteJSON(filepath.Join(dir, "meta.json"), meta); err != nil {
		return fmt.Errorf("write meta for %s: %w", meta.Name, err)
	}
	return d.Heartbeat(meta.Name)
}

// Heartbeat atomically overwrites the heartbeat file with current timestamp.
// If a card.json exists, it also updates the card's LastSeen field.
func (d *Dir) Heartbeat(name string) error {
	path := filepath.Join(d.AgentDir(name), "heartbeat")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := atomicWrite(path, []byte(now+"\n")); err != nil {
		return fmt.Errorf("write heartbeat for %s: %w", name, err)
	}
	// Best-effort card update: if card exists, update LastSeen.
	card, err := d.ReadCard(name)
	if err == nil {
		card.LastSeen = now
		_ = d.writeCardRaw(card)
	}
	return nil
}

// ReadHeartbeat returns the parsed heartbeat time for an agent.
func (d *Dir) ReadHeartbeat(name string) (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(d.AgentDir(name), "heartbeat"))
	if err != nil {
		return time.Time{}, fmt.Errorf("read heartbeat for %s: %w", name, err)
	}
	t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if parseErr != nil {
		return time.Time{}, fmt.Errorf("parse heartbeat for %s: %w", name, parseErr)
	}
	return t, nil
}

// ReadMeta reads an agent's meta.json.
func (d *Dir) ReadMeta(name string) (core.AgentMeta, error) {
	var meta core.AgentMeta
	data, err := os.ReadFile(filepath.Join(d.AgentDir(name), "meta.json"))
	if err != nil {
		return meta, fmt.Errorf("read meta for %s: %w", name, err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, fmt.Errorf("parse meta for %s: %w", name, err)
	}
	return meta, nil
}

// WriteCard writes the agent card as agents/{name}/card.json (atomic write).
// Name is required. If Status is set, it must be a valid agent status.
// LastSeen is automatically set to the current time.
func (d *Dir) WriteCard(card core.AgentCard) error {
	if strings.TrimSpace(card.Name) == "" {
		return fmt.Errorf("card name is required")
	}
	if card.Status != "" && !core.ValidAgentStatuses[card.Status] {
		return fmt.Errorf("invalid agent status: %q", card.Status)
	}
	card.LastSeen = time.Now().UTC().Format(time.RFC3339)
	return d.writeCardRaw(card)
}

// writeCardRaw writes the card without setting LastSeen or validating.
func (d *Dir) writeCardRaw(card core.AgentCard) error {
	dir := d.AgentDir(card.Name)
	return atomicWriteJSON(filepath.Join(dir, "card.json"), card)
}

// ReadCard reads the agent card for an agent.
func (d *Dir) ReadCard(name string) (core.AgentCard, error) {
	var card core.AgentCard
	data, err := os.ReadFile(filepath.Join(d.AgentDir(name), "card.json"))
	if err != nil {
		return card, fmt.Errorf("read card for %s: %w", name, err)
	}
	if err := json.Unmarshal(data, &card); err != nil {
		return card, fmt.Errorf("parse card for %s: %w", name, err)
	}
	return card, nil
}

// ListCards reads cards for all registered agents.
// Agents without a card.json are silently skipped.
func (d *Dir) ListCards() ([]core.AgentCard, error) {
	agents, err := d.ListAgents()
	if err != nil {
		return nil, fmt.Errorf("list agents for cards: %w", err)
	}
	var cards []core.AgentCard
	for _, name := range agents {
		card, err := d.ReadCard(name)
		if err != nil {
			continue // no card yet
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// ReadHeartbeatTime returns the best-known last-seen time for an agent.
// Prefers card.json LastSeen; falls back to the heartbeat file.
func (d *Dir) ReadHeartbeatTime(name string) (time.Time, error) {
	card, err := d.ReadCard(name)
	if err == nil && card.LastSeen != "" {
		t, parseErr := time.Parse(time.RFC3339, card.LastSeen)
		if parseErr == nil {
			return t, nil
		}
	}
	return d.ReadHeartbeat(name)
}

// UpdateTask updates the task field in an agent's meta.json.
func (d *Dir) UpdateTask(name, task string) error {
	meta, err := d.ReadMeta(name)
	if err != nil {
		return fmt.Errorf("update task for %s: %w", name, err)
	}
	meta.Task = task
	return atomicWriteJSON(filepath.Join(d.AgentDir(name), "meta.json"), meta)
}

// ListAgents returns the names of all registered agents.
func (d *Dir) ListAgents() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, "agents"))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read agents dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Send appends a message to the recipient's inbox under flock.
func (d *Dir) Send(msg core.Message) error {
	// Backward-compatibility: normalize legacy ad-hoc messages.
	if msg.ID == "" {
		msg.ID = core.NewULID()
	}
	if msg.TS == "" {
		msg.TS = time.Now().UTC().Format(time.RFC3339)
	}
	if msg.Subject == "" {
		msg.Subject = msg.Body
		if len(msg.Subject) > 80 {
			msg.Subject = msg.Subject[:80]
		}
	}
	if msg.Priority == "" {
		msg.Priority = "normal"
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("validate message to %s: %w", msg.To, err)
	}

	dir := d.AgentDir(msg.To)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("recipient %q not registered", msg.To)
	}

	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message to %s: %w", msg.To, err)
	}
	line = append(line, '\n')

	inbox := filepath.Join(dir, "inbox.jsonl")
	lockPath := inbox + ".lock"
	return flockAppend(lockPath, inbox, line)
}

// ReadInbox reads messages from an agent's inbox, applying filters.
func (d *Dir) ReadInbox(agent string, opts ReadOpts) ([]core.Message, error) {
	inbox := filepath.Join(d.AgentDir(agent), "inbox.jsonl")
	data, err := os.ReadFile(inbox)
	if err != nil {
		if os.IsNotExist(err) {
			return []core.Message{}, nil
		}
		return nil, fmt.Errorf("read inbox for %s: %w", agent, err)
	}

	var offset int64
	if opts.Unread {
		off, readErr := d.readCursor(agent)
		if readErr == nil {
			offset = off
		} else if !errors.Is(readErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("read cursor for %s: %w", agent, readErr)
		}
	}

	var msgs []core.Message
	lines := strings.Split(string(data), "\n")
	var bytePos int64
	for _, line := range lines {
		lineLen := int64(len(line)) + 1 // +1 for newline
		if opts.Unread && bytePos < offset {
			bytePos += lineLen
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			bytePos += lineLen
			continue
		}
		var msg core.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Tolerate partial trailing lines (per review §1.1)
			bytePos += lineLen
			continue
		}
		if opts.match(msg) {
			msgs = append(msgs, msg)
		}
		bytePos += lineLen
	}

	// Apply --last N limit
	if opts.Last > 0 && len(msgs) > opts.Last {
		msgs = msgs[len(msgs)-opts.Last:]
	}

	if opts.MarkRead {
		if err := d.writeCursor(agent, int64(len(data))); err != nil {
			return nil, fmt.Errorf("write cursor for %s: %w", agent, err)
		}
	}

	return msgs, nil
}

// WatchInbox blocks until new messages are appended to an inbox after offset.
// It returns all complete messages written since that offset and the new offset.
func (d *Dir) WatchInbox(agent string, offset int64) ([]core.Message, int64, error) {
	agentDir := d.AgentDir(agent)
	if _, err := os.Stat(agentDir); err != nil {
		if os.IsNotExist(err) {
			return nil, offset, fmt.Errorf("agent %q not registered", agent)
		}
		return nil, offset, fmt.Errorf("stat agent dir for %s: %w", agent, err)
	}

	inbox := filepath.Join(agentDir, "inbox.jsonl")
	f, err := os.OpenFile(inbox, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, offset, fmt.Errorf("open inbox for %s: %w", agent, err)
	}
	if err := f.Close(); err != nil {
		return nil, offset, fmt.Errorf("close inbox for %s: %w", agent, err)
	}

	// If caller starts from zero but file already contains history, watch only new writes.
	if offset == 0 {
		if info, err := os.Stat(inbox); err == nil {
			offset = info.Size()
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, offset, fmt.Errorf("create watcher for %s: %w", agent, err)
	}
	defer watcher.Close()

	if err := watcher.Add(inbox); err != nil {
		return nil, offset, fmt.Errorf("watch inbox for %s: %w", agent, err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil, offset, fmt.Errorf("watcher closed")
			}
			if event.Name != inbox {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			msgs, newOffset, err := readMessagesSince(inbox, offset)
			if err != nil {
				return nil, offset, fmt.Errorf("read new messages for %s: %w", agent, err)
			}
			offset = newOffset
			if len(msgs) > 0 {
				return msgs, offset, nil
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil, offset, fmt.Errorf("watcher closed")
			}
			return nil, offset, fmt.Errorf("watch inbox for %s: %w", agent, err)
		}
	}
}

func readMessagesSince(inbox string, offset int64) ([]core.Message, int64, error) {
	data, err := os.ReadFile(inbox)
	if err != nil {
		return nil, offset, fmt.Errorf("read inbox file: %w", err)
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}

	tail := data[offset:]
	lines := strings.Split(string(tail), "\n")

	var msgs []core.Message
	var consumed int64
	for i, line := range lines {
		// ignore final incomplete line
		if i == len(lines)-1 && line != "" {
			break
		}
		line = strings.TrimSpace(line)
		consumed += int64(len(lines[i])) + 1
		if line == "" {
			continue
		}
		var msg core.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}

	return msgs, offset + consumed, nil
}

// ReadOpts controls message filtering.
type ReadOpts struct {
	From     string
	Thread   string
	Since    time.Time
	Unread   bool
	Last     int
	MarkRead bool
	Type     string
}

func (o ReadOpts) match(msg core.Message) bool {
	if o.From != "" && msg.From != o.From {
		return false
	}
	if o.Thread != "" && msg.Thread != o.Thread {
		return false
	}
	if !o.Since.IsZero() {
		t, err := time.Parse(time.RFC3339, msg.TS)
		if err != nil || t.Before(o.Since) {
			return false
		}
	}
	if o.Type != "" && msg.Type != o.Type {
		return false
	}
	return true
}

func (d *Dir) readCursor(agent string) (int64, error) {
	data, err := os.ReadFile(filepath.Join(d.AgentDir(agent), "cursor"))
	if err != nil {
		return 0, fmt.Errorf("read cursor for %s: %w", agent, err)
	}
	var offset int64
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &offset)
	return offset, nil
}

func (d *Dir) writeCursor(agent string, offset int64) error {
	path := filepath.Join(d.AgentDir(agent), "cursor")
	return atomicWrite(path, []byte(fmt.Sprintf("%d\n", offset)))
}

// ReservationHash returns the SHA-256 hash key for a reservation.
func ReservationHash(repo, pattern string) string {
	h := sha256.Sum256([]byte(repo + ":" + pattern))
	return fmt.Sprintf("%x", h)
}

// Reserve creates a reservation atomically with O_CREAT|O_EXCL.
func (d *Dir) Reserve(res core.Reservation) error {
	hash := ReservationHash(res.Repo, res.Pattern)
	path := filepath.Join(d.Root, "reservations", hash+".json")

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal reservation %s: %w", res.Pattern, err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			// Read existing to report conflict
			existing, readErr := d.readReservation(path)
			if readErr == nil {
				return fmt.Errorf("conflict: %s already reserved by %s (%s)", res.Pattern, existing.Agent, existing.Reason)
			}
			return fmt.Errorf("conflict: %s already reserved", res.Pattern)
		}
		return fmt.Errorf("create reservation file for %s: %w", res.Pattern, err)
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("write reservation %s: %w", res.Pattern, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close reservation file %s: %w", res.Pattern, closeErr)
	}
	return nil
}

// Release removes a reservation. Returns error if not owned by agent.
func (d *Dir) Release(agent, repo, pattern string) error {
	hash := ReservationHash(repo, pattern)
	path := filepath.Join(d.Root, "reservations", hash+".json")

	res, err := d.readReservation(path)
	if err != nil {
		return fmt.Errorf("reservation not found for pattern %q", pattern)
	}
	if res.Agent != agent {
		return fmt.Errorf("reservation owned by %s, not %s", res.Agent, agent)
	}
	return os.Remove(path)
}

// ReleaseAll removes all reservations held by an agent.
func (d *Dir) ReleaseAll(agent string) (int, error) {
	reservations, err := d.ListReservations()
	if err != nil {
		return 0, fmt.Errorf("list reservations for release-all: %w", err)
	}
	count := 0
	for _, r := range reservations {
		if r.Agent == agent {
			hash := ReservationHash(r.Repo, r.Pattern)
			path := filepath.Join(d.Root, "reservations", hash+".json")
			if err := os.Remove(path); err == nil {
				count++
			} // best-effort: skip reservations that can't be removed
		}
	}
	return count, nil
}

// ListReservations reads all reservation files.
func (d *Dir) ListReservations() ([]core.Reservation, error) {
	dir := filepath.Join(d.Root, "reservations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []core.Reservation{}, nil
		}
		return nil, fmt.Errorf("read reservations dir: %w", err)
	}
	var result []core.Reservation
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, err := d.readReservation(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

func (d *Dir) readReservation(path string) (core.Reservation, error) {
	var r core.Reservation
	data, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("read reservation file %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parse reservation file %s: %w", filepath.Base(path), err)
	}
	return r, nil
}

// CheckOverlap checks if a new pattern overlaps with any existing reservation
// from a different agent. Uses prefix-based overlap detection (per review §3.2).
func (d *Dir) CheckOverlap(agent, repo, pattern string) ([]core.Reservation, error) {
	existing, err := d.ListReservations()
	if err != nil {
		return nil, fmt.Errorf("list reservations for overlap check: %w", err)
	}
	var conflicts []core.Reservation
	for _, r := range existing {
		if r.Agent == agent || r.Repo != repo {
			continue
		}
		if isExpired(r) {
			continue
		}
		if patternsOverlap(pattern, r.Pattern) {
			conflicts = append(conflicts, r)
		}
	}
	return conflicts, nil
}

// patternsOverlap checks if two glob patterns could match the same path.
// Uses conservative prefix-based detection per review recommendation.
func patternsOverlap(a, b string) bool {
	// Exact match
	if a == b {
		return true
	}

	// If either is a recursive catch-all **, it overlaps with everything
	if a == "**" || b == "**" {
		return true
	}

	// Normalize: strip trailing /**
	aNorm := strings.TrimSuffix(a, "/**")
	bNorm := strings.TrimSuffix(b, "/**")

	// Prefix check: src/auth/** overlaps with src/auth/login.go
	if strings.HasPrefix(aNorm, bNorm+"/") || strings.HasPrefix(bNorm, aNorm+"/") {
		return true
	}
	if strings.HasPrefix(a, bNorm+"/") || strings.HasPrefix(b, aNorm+"/") {
		return true
	}

	// Check if both patterns are in the same directory and could match the same file.
	// *.go only matches in the root dir, not subdirs — it doesn't cross /.
	// src/*.go only matches in src/, not src/sub/.
	aDir := filepath.Dir(a)
	bDir := filepath.Dir(b)
	if aDir == bDir {
		// Same directory — check extension overlap for wildcard patterns
		aBase := filepath.Base(a)
		bBase := filepath.Base(b)
		if strings.HasPrefix(aBase, "*.") || strings.HasPrefix(bBase, "*.") {
			aExt := filepath.Ext(a)
			bExt := filepath.Ext(b)
			if aExt != "" && bExt != "" && aExt == bExt {
				return true
			}
		}
	}

	// Recursive glob (**) in one pattern vs a concrete path or extension wildcard in another
	if strings.Contains(a, "**") && !strings.Contains(b, "**") {
		// a is recursive, b is concrete — a's prefix matches b's directory?
		aPrefix := strings.SplitN(a, "**", 2)[0]
		if strings.HasPrefix(b, aPrefix) {
			return true
		}
	}
	if strings.Contains(b, "**") && !strings.Contains(a, "**") {
		bPrefix := strings.SplitN(b, "**", 2)[0]
		if strings.HasPrefix(a, bPrefix) {
			return true
		}
	}

	return false
}

func isExpired(r core.Reservation) bool {
	t, err := time.Parse(time.RFC3339, r.ExpiresAt)
	if err != nil {
		return true // unparseable timestamp → treat as expired so GC can collect it
	}
	return time.Now().After(t)
}

// CreateCommand atomically creates a command file.
func (d *Dir) CreateCommand(cmd core.Command) error {
	path := filepath.Join(d.Root, "commands", cmd.ID+".json")
	data, err := json.MarshalIndent(cmd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal command %s: %w", cmd.ID, err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create command file %s: %w", cmd.ID, err)
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("write command %s: %w", cmd.ID, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close command file %s: %w", cmd.ID, closeErr)
	}
	return nil
}

// ConsumeCommand claims a command using a .consumed sidecar (per review §1.3).
// Returns true if this caller won the claim.
func (d *Dir) ConsumeCommand(id string) (bool, error) {
	sidecar := filepath.Join(d.Root, "commands", id+".consumed")
	f, err := os.OpenFile(sidecar, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil // already consumed
		}
		return false, fmt.Errorf("create consumed sidecar for %s: %w", id, err)
	}
	if _, err := f.Write([]byte(time.Now().UTC().Format(time.RFC3339) + "\n")); err != nil {
		_ = f.Close()
		_ = os.Remove(sidecar)
		return false, fmt.Errorf("write consumed sidecar for %s: %w", id, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(sidecar)
		return false, fmt.Errorf("close consumed sidecar for %s: %w", id, err)
	}
	return true, nil
}

// ListCommands reads all pending command files.
func (d *Dir) ListCommands() ([]core.Command, error) {
	dir := filepath.Join(d.Root, "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []core.Command{}, nil
		}
		return nil, fmt.Errorf("read commands dir: %w", err)
	}
	var cmds []core.Command
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var cmd core.Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			continue
		}
		// Check if consumed
		sidecar := filepath.Join(dir, strings.TrimSuffix(e.Name(), ".json")+".consumed")
		if _, err := os.Stat(sidecar); err == nil {
			cmd.Status = "consumed"
		}
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

// GC removes expired reservations and old consumed commands.
func (d *Dir) GC(staleThreshold time.Duration, expiredOnly bool) (GCResult, error) {
	var result GCResult

	// Clean expired reservations
	reservations, err := d.ListReservations()
	if err != nil {
		return result, fmt.Errorf("gc list reservations: %w", err)
	}
	for _, r := range reservations {
		if isExpired(r) {
			hash := ReservationHash(r.Repo, r.Pattern)
			path := filepath.Join(d.Root, "reservations", hash+".json")
			if err := os.Remove(path); err == nil {
				result.ExpiredReservations++
			}
		}
	}

	// Clean old consumed commands (older than 1 hour)
	cmds, err := d.ListCommands()
	if err != nil {
		return result, fmt.Errorf("gc list commands: %w", err)
	}
	for _, cmd := range cmds {
		sidecar := filepath.Join(d.Root, "commands", cmd.ID+".consumed")
		if info, err := os.Stat(sidecar); err == nil {
			if time.Since(info.ModTime()) > time.Hour {
				// best-effort: remove old command files
				_ = os.Remove(filepath.Join(d.Root, "commands", cmd.ID+".json"))
				_ = os.Remove(sidecar)
				result.OldCommands++
			}
		}
	}

	// Archive stale agents
	if !expiredOnly {
		agents, err := d.ListAgents()
		if err != nil {
			return result, fmt.Errorf("gc list agents: %w", err)
		}
		for _, name := range agents {
			hb, err := d.ReadHeartbeat(name)
			if err != nil {
				continue
			}
			if time.Since(hb) > staleThreshold {
				// Mark as stale by renaming heartbeat
				src := filepath.Join(d.AgentDir(name), "heartbeat")
				dst := filepath.Join(d.AgentDir(name), "heartbeat.stale")
				if os.Rename(src, dst) == nil {
					result.StaleAgents++
				}
			}
		}
	}

	return result, nil
}

// GCResult reports what GC cleaned up.
type GCResult struct {
	ExpiredReservations int `json:"expired_reservations"`
	OldCommands         int `json:"old_commands"`
	StaleAgents         int `json:"stale_agents"`
}

// Metrics holds aggregate system metrics.
type Metrics struct {
	Agents              int `json:"agents"`
	AliveAgents         int `json:"alive_agents"`
	StaleAgents         int `json:"stale_agents"`
	TotalMessages       int `json:"total_messages"`
	Reservations        int `json:"reservations"`
	ActiveReservations  int `json:"active_reservations"`
	ExpiredReservations int `json:"expired_reservations"`
	Commands            int `json:"commands"`
	PendingCommands     int `json:"pending_commands"`
}

// Metrics computes aggregate system metrics.
func (d *Dir) Metrics(staleThreshold time.Duration) (Metrics, error) {
	var m Metrics

	agents, err := d.ListAgents()
	if err != nil {
		return m, fmt.Errorf("metrics list agents: %w", err)
	}
	m.Agents = len(agents)
	for _, name := range agents {
		hb, err := d.ReadHeartbeatTime(name)
		if err != nil || time.Since(hb) > staleThreshold {
			m.StaleAgents++
		} else {
			m.AliveAgents++
		}
	}

	// Count messages across all inboxes
	for _, name := range agents {
		inbox := filepath.Join(d.AgentDir(name), "inbox.jsonl")
		data, err := os.ReadFile(inbox)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				m.TotalMessages++
			}
		}
	}

	// Count reservations
	reservations, err := d.ListReservations()
	if err != nil {
		return m, fmt.Errorf("metrics list reservations: %w", err)
	}
	m.Reservations = len(reservations)
	now := time.Now()
	for _, r := range reservations {
		expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
		if err != nil || now.After(expires) {
			m.ExpiredReservations++
		} else {
			m.ActiveReservations++
		}
	}

	// Count commands
	cmds, err := d.ListCommands()
	if err != nil {
		return m, fmt.Errorf("metrics list commands: %w", err)
	}
	m.Commands = len(cmds)
	for _, cmd := range cmds {
		if cmd.Status == "pending" {
			m.PendingCommands++
		}
	}

	return m, nil
}

// TouchWake touches the wake trigger file.
func (d *Dir) TouchWake(text string) error {
	triggerPath := filepath.Join(d.Root, "wake", "trigger")
	if err := atomicWrite(triggerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n")); err != nil {
		return fmt.Errorf("write wake trigger: %w", err)
	}
	if text != "" {
		msgPath := filepath.Join(d.Root, "wake", "last-message")
		return atomicWrite(msgPath, []byte(text+"\n"))
	}
	return nil
}

// atomicWrite writes data to a temp file then renames atomically.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".relay-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file for %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file for %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", filepath.Base(path), err)
	}
	return nil
}

func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON for %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}

// flockAppend acquires an exclusive flock on lockPath, then appends data to dataPath.
func flockAppend(lockPath, dataPath string, data []byte) error {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	f, err := os.OpenFile(dataPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open data file: %w", err)
	}
	defer f.Close()

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("append to %s: %w", filepath.Base(dataPath), err)
	}
	return nil
}
