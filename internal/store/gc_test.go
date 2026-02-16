package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Perttulands/relay/internal/core"
)

func TestGCStaleAgents(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent-old", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Backdate the heartbeat
	hbPath := filepath.Join(d.AgentDir("agent-old"), "heartbeat")
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	atomicWrite(hbPath, []byte(old+"\n"))

	result, err := d.GC(30*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.StaleAgents != 1 {
		t.Errorf("expected 1 stale agent archived, got %d", result.StaleAgents)
	}

	// heartbeat.stale should exist, heartbeat should not
	if _, err := os.Stat(filepath.Join(d.AgentDir("agent-old"), "heartbeat.stale")); err != nil {
		t.Error("expected heartbeat.stale to exist")
	}
	if _, err := os.Stat(hbPath); !os.IsNotExist(err) {
		t.Error("expected heartbeat to be removed")
	}
}

func TestGCOldConsumedCommands(t *testing.T) {
	d := tempDir(t)

	cmd := core.Command{
		ID:            core.NewULID(),
		TS:            time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		From:          "agent-1",
		TargetSession: "target",
		Command:       "/test",
		Status:        "pending",
	}
	d.CreateCommand(cmd)
	d.ConsumeCommand(cmd.ID)

	// Backdate the .consumed sidecar
	sidecar := filepath.Join(d.Root, "commands", cmd.ID+".consumed")
	os.Chtimes(sidecar, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))

	result, err := d.GC(30*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.OldCommands != 1 {
		t.Errorf("expected 1 old command cleaned, got %d", result.OldCommands)
	}

	// Both files should be gone
	if _, err := os.Stat(filepath.Join(d.Root, "commands", cmd.ID+".json")); !os.IsNotExist(err) {
		t.Error("expected command JSON to be removed")
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Error("expected sidecar to be removed")
	}
}

func TestGCExpiredOnlySkipsAgents(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "stale", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Backdate heartbeat
	hbPath := filepath.Join(d.AgentDir("stale"), "heartbeat")
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	atomicWrite(hbPath, []byte(old+"\n"))

	result, _ := d.GC(30*time.Minute, true) // expired-only
	if result.StaleAgents != 0 {
		t.Errorf("expected 0 stale agents with expired-only, got %d", result.StaleAgents)
	}
}

func TestReadSinceFilter(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Old message
	d.Send(core.Message{
		ID:   core.NewULID(),
		TS:   time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "old",
	})
	// Recent message
	d.Send(core.Message{
		ID:   core.NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "new",
	})

	msgs, _ := d.ReadInbox("alice", ReadOpts{Since: time.Now().Add(-30 * time.Minute)})
	if len(msgs) != 1 {
		t.Errorf("expected 1 recent message, got %d", len(msgs))
	}
	if len(msgs) > 0 && msgs[0].Body != "new" {
		t.Errorf("expected body=new, got %s", msgs[0].Body)
	}
}

func TestCheckOverlapExpiredReservation(t *testing.T) {
	d := tempDir(t)
	// Expired reservation should not trigger conflict
	d.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "src/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	conflicts, _ := d.CheckOverlap("agent-2", "/repo", "src/main.go")
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts with expired reservation, got %d", len(conflicts))
	}
}

func TestTouchWakeNoText(t *testing.T) {
	d := tempDir(t)
	if err := d.TouchWake(""); err != nil {
		t.Fatal(err)
	}
	// Trigger should exist, last-message should NOT
	if _, err := os.Stat(filepath.Join(d.Root, "wake", "trigger")); err != nil {
		t.Error("trigger not created")
	}
}
