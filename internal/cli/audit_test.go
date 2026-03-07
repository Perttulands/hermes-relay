package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/store"
)

func TestLogActivationWritesJSONL(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	logActivation(s, "hestia", "hermes", "chain-123", 1, "delivered", "")

	entries, err := s.ReadActivationLog(store.LogReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Sender != "hestia" || entries[0].Target != "hermes" || entries[0].Outcome != "delivered" {
		t.Errorf("unexpected entry: %+v", entries[0])
	}
}

func TestLogTailReturnsLastN(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write 5 entries
	for i := 0; i < 5; i++ {
		logActivation(s, "sender", "target", "chain-1", i+1, "delivered", "")
	}

	code, out := captureRun(t, "log", "--tail", "3")
	if code != 0 {
		t.Fatalf("log --tail 3 failed with code %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
}

func TestLogTailDefault(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write 25 entries
	for i := 0; i < 25; i++ {
		logActivation(s, "sender", "target", "chain-1", i+1, "delivered", "")
	}

	code, out := captureRun(t, "log", "--tail")
	if code != 0 {
		t.Fatalf("log --tail failed with code %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 20 {
		t.Fatalf("expected 20 lines (default tail), got %d", len(lines))
	}
}

func TestLogChainFilters(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	logActivation(s, "hestia", "hermes", "chain-abc", 1, "delivered", "")
	logActivation(s, "hermes", "iris", "chain-abc", 2, "delivered", "")
	logActivation(s, "athena", "codex", "chain-xyz", 1, "delivered", "")

	code, out := captureRun(t, "log", "--chain", "chain-abc")
	if code != 0 {
		t.Fatalf("log --chain failed with code %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines for chain-abc, got %d: %q", len(lines), out)
	}
	if !strings.Contains(out, "hestia") || !strings.Contains(out, "hermes") {
		t.Errorf("output should contain chain-abc hops: %q", out)
	}
}

func TestSpendTodayAggregates(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write entries with today's timestamp
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		_ = s.AppendActivationLog(store.ActivationLogEntry{
			TS:      now,
			Sender:  "hestia",
			Target:  "hermes",
			Outcome: "delivered",
		})
	}
	_ = s.AppendActivationLog(store.ActivationLogEntry{
		TS:      now,
		Sender:  "hestia",
		Target:  "iris",
		Outcome: "delivered",
	})
	// Non-delivered should not count
	_ = s.AppendActivationLog(store.ActivationLogEntry{
		TS:      now,
		Sender:  "hestia",
		Target:  "hermes",
		Outcome: "throttled",
	})

	code, out := captureRun(t, "spend", "--today")
	if code != 0 {
		t.Fatalf("spend --today failed with code %d", code)
	}

	if !strings.Contains(out, "hermes") || !strings.Contains(out, "3") {
		t.Errorf("expected hermes with 3 wakes, got: %q", out)
	}
	if !strings.Contains(out, "iris") || !strings.Contains(out, "1") {
		t.Errorf("expected iris with 1 wake, got: %q", out)
	}
	if !strings.Contains(out, "4 total") {
		t.Errorf("expected 4 total, got: %q", out)
	}
}

func TestSpendJSON(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.AppendActivationLog(store.ActivationLogEntry{
		TS: now, Sender: "a", Target: "b", Outcome: "delivered",
	})

	code, out := captureRun(t, "spend", "--today", "--json")
	if code != 0 {
		t.Fatalf("spend --today --json failed with code %d", code)
	}

	var counts map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &counts); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %q", err, out)
	}
	if counts["b"] != 1 {
		t.Errorf("expected b=1, got %v", counts)
	}
}

func TestSpendAgentFilter(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.AppendActivationLog(store.ActivationLogEntry{
		TS: now, Sender: "a", Target: "hermes", Outcome: "delivered",
	})
	_ = s.AppendActivationLog(store.ActivationLogEntry{
		TS: now, Sender: "a", Target: "iris", Outcome: "delivered",
	})

	code, out := captureRun(t, "spend", "--target", "hermes")
	if code != 0 {
		t.Fatalf("spend --agent failed with code %d", code)
	}

	if !strings.Contains(out, "hermes") {
		t.Errorf("expected hermes in output: %q", out)
	}
	if strings.Contains(out, "iris") {
		t.Errorf("expected iris NOT in output: %q", out)
	}
}

func TestLogEmpty(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code, out := captureRun(t, "log", "--tail")
	if code != 0 {
		t.Fatalf("log --tail on empty log failed with code %d", code)
	}
	if !strings.Contains(strings.ToLower(out), "no activations") {
		t.Errorf("expected 'no activations' for empty log, got: %q", out)
	}
}

func TestSpendNoFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("spend")
	if code != 1 {
		t.Errorf("expected exit 1 for spend without flags, got %d", code)
	}
}

func TestLogNoFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("log")
	if code != 1 {
		t.Errorf("expected exit 1 for log without flags, got %d", code)
	}
}
