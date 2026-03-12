package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ActivationLogEntry represents a single wake attempt in the activation log.
type ActivationLogEntry struct {
	TS         string `json:"ts"`
	Sender     string `json:"sender"`
	Target     string `json:"target"`
	ChainID    string `json:"chain_id,omitempty"`
	Depth      int    `json:"depth,omitempty"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	GatewayURL string `json:"gateway_url,omitempty"`
}

// HarbourAuditEntry is a single external relay_send event.
type HarbourAuditEntry struct {
	TS         string `json:"ts"`
	From       string `json:"from"`
	To         string `json:"to"`
	Action     string `json:"action"`
	TrustLevel int    `json:"trust_level"`
	ID         string `json:"id"`
}

// LogReadOpts controls filtering when reading the activation log.
type LogReadOpts struct {
	ChainID   string
	Tail      int
	StartDate time.Time // inclusive
	EndDate   time.Time // exclusive
	Agent     string    // filter by target agent
}

func (d *Dir) activationLogPath() string {
	return filepath.Join(d.Root, "activation-log.jsonl")
}

func (d *Dir) harbourAuditPath() string {
	return filepath.Join(d.Root, "harbour-audit.jsonl")
}

// AppendActivationLog appends a single entry to the activation log under flock.
func (d *Dir) AppendActivationLog(entry ActivationLogEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal activation log entry: %w", err)
	}
	line = append(line, '\n')

	logPath := d.activationLogPath()
	lockPath := logPath + ".lock"
	return flockAppend(lockPath, logPath, line)
}

// AppendHarbourAuditLog appends a single entry to harbour-audit.jsonl under flock.
func (d *Dir) AppendHarbourAuditLog(entry HarbourAuditEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal harbour audit entry: %w", err)
	}
	line = append(line, '\n')

	logPath := d.harbourAuditPath()
	lockPath := logPath + ".lock"
	return flockAppend(lockPath, logPath, line)
}

// ReadActivationLog reads entries from the activation log, applying filters.
func (d *Dir) ReadActivationLog(opts LogReadOpts) ([]ActivationLogEntry, error) {
	data, err := os.ReadFile(d.activationLogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []ActivationLogEntry{}, nil
		}
		return nil, fmt.Errorf("read activation log: %w", err)
	}

	var entries []ActivationLogEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry ActivationLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // tolerate corrupt lines
		}

		// Apply filters
		if opts.ChainID != "" && entry.ChainID != opts.ChainID {
			continue
		}
		if opts.Agent != "" && entry.Target != opts.Agent {
			continue
		}
		if !opts.StartDate.IsZero() || !opts.EndDate.IsZero() {
			ts, err := time.Parse(time.RFC3339, entry.TS)
			if err != nil {
				continue
			}
			if !opts.StartDate.IsZero() && ts.Before(opts.StartDate) {
				continue
			}
			if !opts.EndDate.IsZero() && !ts.Before(opts.EndDate) {
				continue
			}
		}

		entries = append(entries, entry)
	}

	// Apply tail
	if opts.Tail > 0 && len(entries) > opts.Tail {
		entries = entries[len(entries)-opts.Tail:]
	}

	return entries, nil
}
