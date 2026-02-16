// Package store implements filesystem operations for relay.
// All writes use atomic rename or flock-guarded append.
package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Perttulands/relay/internal/core"
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
		return err
	}
	if err := atomicWriteJSON(filepath.Join(dir, "meta.json"), meta); err != nil {
		return err
	}
	return d.Heartbeat(meta.Name)
}

// Heartbeat atomically overwrites the heartbeat file with current timestamp.
func (d *Dir) Heartbeat(name string) error {
	path := filepath.Join(d.AgentDir(name), "heartbeat")
	return atomicWrite(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

// ReadHeartbeat returns the parsed heartbeat time for an agent.
func (d *Dir) ReadHeartbeat(name string) (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(d.AgentDir(name), "heartbeat"))
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
}

// ReadMeta reads an agent's meta.json.
func (d *Dir) ReadMeta(name string) (core.AgentMeta, error) {
	var meta core.AgentMeta
	data, err := os.ReadFile(filepath.Join(d.AgentDir(name), "meta.json"))
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// UpdateTask updates the task field in an agent's meta.json.
func (d *Dir) UpdateTask(name, task string) error {
	meta, err := d.ReadMeta(name)
	if err != nil {
		return err
	}
	meta.Task = task
	return atomicWriteJSON(filepath.Join(d.AgentDir(name), "meta.json"), meta)
}

// ListAgents returns the names of all registered agents.
func (d *Dir) ListAgents() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, "agents"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
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
	if len(msg.Body) > core.MaxBodySize {
		return fmt.Errorf("message body too large: %d bytes (max %d)", len(msg.Body), core.MaxBodySize)
	}
	dir := d.AgentDir(msg.To)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("recipient %q not registered", msg.To)
	}

	line, err := json.Marshal(msg)
	if err != nil {
		return err
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
			return nil, nil
		}
		return nil, err
	}

	var offset int64
	if opts.Unread {
		offset, _ = d.readCursor(agent)
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
		_ = d.writeCursor(agent, int64(len(data)))
	}

	return msgs, nil
}

// ReadOpts controls message filtering.
type ReadOpts struct {
	From     string
	Thread   string
	Since    time.Time
	Unread   bool
	Last     int
	MarkRead bool
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
	return true
}

func (d *Dir) readCursor(agent string) (int64, error) {
	data, err := os.ReadFile(filepath.Join(d.AgentDir(agent), "cursor"))
	if err != nil {
		return 0, err
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
		return err
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
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
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
		return 0, err
	}
	count := 0
	for _, r := range reservations {
		if r.Agent == agent {
			hash := ReservationHash(r.Repo, r.Pattern)
			path := filepath.Join(d.Root, "reservations", hash+".json")
			if err := os.Remove(path); err == nil {
				count++
			}
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
			return nil, nil
		}
		return nil, err
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
		return r, err
	}
	err = json.Unmarshal(data, &r)
	return r, err
}

// CheckOverlap checks if a new pattern overlaps with any existing reservation
// from a different agent. Uses prefix-based overlap detection (per review §3.2).
func (d *Dir) CheckOverlap(agent, repo, pattern string) ([]core.Reservation, error) {
	existing, err := d.ListReservations()
	if err != nil {
		return nil, err
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
		return false
	}
	return time.Now().After(t)
}

// CreateCommand atomically creates a command file.
func (d *Dir) CreateCommand(cmd core.Command) error {
	path := filepath.Join(d.Root, "commands", cmd.ID+".json")
	data, err := json.MarshalIndent(cmd, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
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
		return false, err
	}
	_, _ = f.Write([]byte(time.Now().UTC().Format(time.RFC3339) + "\n"))
	f.Close()
	return true, nil
}

// ListCommands reads all pending command files.
func (d *Dir) ListCommands() ([]core.Command, error) {
	dir := filepath.Join(d.Root, "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
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
		return result, err
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
	cmds, _ := d.ListCommands()
	for _, cmd := range cmds {
		sidecar := filepath.Join(d.Root, "commands", cmd.ID+".consumed")
		if info, err := os.Stat(sidecar); err == nil {
			if time.Since(info.ModTime()) > time.Hour {
				os.Remove(filepath.Join(d.Root, "commands", cmd.ID+".json"))
				os.Remove(sidecar)
				result.OldCommands++
			}
		}
	}

	// Archive stale agents
	if !expiredOnly {
		agents, _ := d.ListAgents()
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

// TouchWake touches the wake trigger file.
func (d *Dir) TouchWake(text string) error {
	triggerPath := filepath.Join(d.Root, "wake", "trigger")
	if err := atomicWrite(triggerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n")); err != nil {
		return err
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
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
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

	_, err = f.Write(data)
	return err
}
