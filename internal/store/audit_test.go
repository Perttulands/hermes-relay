package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendActivationLog(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	entry := ActivationLogEntry{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Sender:  "hestia",
		Target:  "hermes",
		ChainID: "test-chain-1",
		Depth:   1,
		Outcome: "delivered",
	}
	if err := s.AppendActivationLog(entry); err != nil {
		t.Fatalf("AppendActivationLog: %v", err)
	}

	// Read file directly and verify JSONL
	data, err := os.ReadFile(s.activationLogPath())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var got ActivationLogEntry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Sender != "hestia" || got.Target != "hermes" || got.Outcome != "delivered" {
		t.Errorf("unexpected entry: %+v", got)
	}
}

func TestReadActivationLogTail(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write 10 entries
	for i := 0; i < 10; i++ {
		entry := ActivationLogEntry{
			TS:      time.Now().UTC().Format(time.RFC3339),
			Sender:  "sender",
			Target:  "target",
			ChainID: "chain-1",
			Depth:   i + 1,
			Outcome: "delivered",
		}
		if err := s.AppendActivationLog(entry); err != nil {
			t.Fatal(err)
		}
	}

	// Read last 3
	entries, err := s.ReadActivationLog(LogReadOpts{Tail: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Should be the last 3 (depth 8, 9, 10)
	if entries[0].Depth != 8 {
		t.Errorf("expected depth 8, got %d", entries[0].Depth)
	}
}

func TestReadActivationLogChainFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, cid := range []string{"chain-a", "chain-b", "chain-a"} {
		entry := ActivationLogEntry{
			TS:      time.Now().UTC().Format(time.RFC3339),
			Sender:  "sender",
			Target:  "target",
			ChainID: cid,
			Outcome: "delivered",
		}
		if err := s.AppendActivationLog(entry); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := s.ReadActivationLog(LogReadOpts{ChainID: "chain-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for chain-a, got %d", len(entries))
	}
}

func TestReadActivationLogDateRange(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write entries at different times
	ts1 := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	ts3 := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)

	for _, ts := range []time.Time{ts1, ts2, ts3} {
		entry := ActivationLogEntry{
			TS:      ts.Format(time.RFC3339),
			Sender:  "sender",
			Target:  "target",
			Outcome: "delivered",
		}
		if err := s.AppendActivationLog(entry); err != nil {
			t.Fatal(err)
		}
	}

	// Only March 7
	start := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	entries, err := s.ReadActivationLog(LogReadOpts{StartDate: start, EndDate: end})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for March 7, got %d", len(entries))
	}
}

func TestReadActivationLogAgentFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"hermes", "iris", "hermes"} {
		entry := ActivationLogEntry{
			TS:      time.Now().UTC().Format(time.RFC3339),
			Sender:  "hestia",
			Target:  target,
			Outcome: "delivered",
		}
		if err := s.AppendActivationLog(entry); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := s.ReadActivationLog(LogReadOpts{Agent: "hermes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for hermes, got %d", len(entries))
	}
}

func TestReadActivationLogEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := s.ReadActivationLog(LogReadOpts{Tail: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestAppendHarbourAuditLog(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	entry := HarbourAuditEntry{
		TS:         time.Now().UTC().Format(time.RFC3339),
		From:       "codex",
		To:         "athena",
		Action:     "relay_send",
		TrustLevel: 1,
		ID:         "01KK76QKGMJ3WS6JZ4F3NAQQWT",
	}
	if err := s.AppendHarbourAuditLog(entry); err != nil {
		t.Fatalf("AppendHarbourAuditLog: %v", err)
	}

	data, err := os.ReadFile(s.harbourAuditPath())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var got HarbourAuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.From != "codex" || got.To != "athena" || got.Action != "relay_send" || got.TrustLevel != 1 {
		t.Errorf("unexpected entry: %+v", got)
	}
}
