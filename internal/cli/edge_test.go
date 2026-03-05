package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
	"github.com/Perttulands/hermes-relay/internal/store"
)

func TestSendWithAllFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code := run("send", "target", "msg body",
		"--subject", "custom subject",
		"--thread", "thread-1",
		"--priority", "urgent",
		"--tag", "tag1,tag2")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSendWake(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	// --wake will fail (no gateway) but message should still send
	code := run("send", "target", "msg", "--wake")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadEmptyInbox(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code := run("read")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadWithSince(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "hello", "--agent", "sender")

	code := run("read", "--since", "1h")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}

	code = run("read", "--since", "2026-02-16T00:00:00Z")
	if code != 0 {
		t.Errorf("expected 0 for RFC3339 since, got %d", code)
	}

	code = run("read", "--since", "2026-02-16")
	if code != 0 {
		t.Errorf("expected 0 for date since, got %d", code)
	}
}

func TestReadWithLast(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	for i := 0; i < 5; i++ {
		run("send", "test-agent", "msg", "--agent", "sender")
	}

	code := run("read", "--last", "2")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadWithThread(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "threaded msg", "--agent", "sender", "--thread", "t1")

	code := run("read", "--thread", "t1")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadWithFrom(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "hello", "--agent", "sender")

	code := run("read", "--from", "sender")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsEmpty(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reservations")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsWithFilters(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("reserve", "a.go", "--repo", "/tmp/r")

	code := run("reservations", "--repo", "/tmp/r")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}

	code = run("reservations", "--agent", "test-agent")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}

	code = run("reservations", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsExpired(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Manually create an expired reservation
	s, _ := store.New(dir)
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "old.go",
		Repo:      "/tmp/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	// Without --expired, should show nothing
	code := run("reservations")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}

	// With --expired, should show it
	code = run("reservations", "--expired")
	if code != 0 {
		t.Errorf("expected 0 with --expired, got %d", code)
	}
}

func TestReserveForce(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "force.go", "--repo", "/tmp/r", "--agent", "agent-1")
	// Force override
	code := run("reserve", "force.go", "--repo", "/tmp/r", "--agent", "agent-2", "--force")
	if code != 0 {
		t.Errorf("expected 0 with --force, got %d", code)
	}
}

func TestReserveShared(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reserve", "shared.go", "--repo", "/tmp/r", "--shared", "--ttl", "2h")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reserve", "json.go", "--repo", "/tmp/r", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReleaseNoPattern(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("release")
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestReleaseNonexistent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("release", "nope.go", "--repo", "/tmp/r")
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestStatusWithReservationsAndCommands(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1", "--task", "testing")
	run("reserve", "f.go", "--repo", "/tmp/r", "--agent", "agent-1")
	run("cmd", "target", "/test", "--agent", "agent-1")

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusStaleAgent(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "stale-agent", "--task", "old work")
	// Backdate heartbeat
	hbPath := filepath.Join(dir, "agents", "stale-agent", "heartbeat")
	old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	os.WriteFile(hbPath, []byte(old+"\n"), 0644)

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestCmdWithWake(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// --wake will fail silently (no gateway) but command should still post
	code := run("cmd", "target", "/verify", "--wake")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestCmdJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("cmd", "target", "/test", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestGCWithStale(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("gc", "--stale", "1m")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestGCJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("gc", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestWakeAutoMode(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Auto mode: tries gateway first, falls back to file. Either way should succeed.
	code := run("wake", "test")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestWakeUnknownMethod(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("wake", "--method", "invalid")
	if code != 1 {
		t.Errorf("expected 1 for invalid method, got %d", code)
	}
}

func TestHeartbeatNoAgent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Agent dir doesn't exist
	code := run("heartbeat", "--agent", "nonexistent")
	if code != 1 {
		t.Errorf("expected 1 for nonexistent agent heartbeat, got %d", code)
	}
}

func TestReadLongBody(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")

	// Send a message with a long body that differs from subject
	longBody := "This is a long message body that is different from the subject. " +
		"It contains multiple sentences and should be truncated if over 200 chars. " +
		"More text here to pad it out a bit more and more and more."
	run("send", "test-agent", longBody, "--agent", "sender", "--subject", "Short subject")

	code := run("read")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSendQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code := run("send", "target", "quiet msg", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSendJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code := run("send", "target", "json msg", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveNoPattern(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reserve")
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestRegisterJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "json-agent", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestRegisterQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "quiet-agent", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusWithStaleThreshold(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent", "--task", "work")
	code := run("status", "--stale", "1s")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSendWithTypeAndPayload(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code := run("send", "target", "task done",
		"--type", "task_result",
		"--payload", `{"exit_code":0,"files_changed":3}`)
	if code != 0 {
		t.Fatalf("send with type/payload failed with code %d", code)
	}

	// Read the inbox and verify the message was stored with type and payload
	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var msg core.Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "task_result" {
		t.Errorf("expected type=task_result, got %q", msg.Type)
	}
	if string(msg.Payload) != `{"exit_code":0,"files_changed":3}` {
		t.Errorf("unexpected payload: %s", msg.Payload)
	}
}

func TestSendWithTypeOnly(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code := run("send", "target", "status update", "--type", "status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSendWithoutTypeStillWorks(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	// Backward compat: no --type flag
	code := run("send", "target", "plain message")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadWithTypeFilter(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")

	// Send messages with different types
	run("send", "test-agent", "alert msg", "--agent", "sender", "--type", "alert")
	run("send", "test-agent", "chat msg", "--agent", "sender", "--type", "chat")
	run("send", "test-agent", "plain msg", "--agent", "sender")

	// Read filtering by type=alert
	code := run("read", "--type", "alert", "--json")
	if code != 0 {
		t.Fatalf("read --type alert failed with code %d", code)
	}

	// Read all (no filter)
	code = run("read", "--json")
	if code != 0 {
		t.Fatalf("read all failed with code %d", code)
	}
}

func TestSendInvalidPayloadJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	// Invalid JSON payload should fail validation
	code := run("send", "target", "bad payload", "--type", "task_result", "--payload", `{not json}`)
	if code != 1 {
		t.Errorf("expected exit 1 for invalid payload JSON, got %d", code)
	}
}

func TestSendInvalidType(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	// Invalid type should fail validation
	code := run("send", "target", "bad type", "--type", "nonexistent")
	if code != 1 {
		t.Errorf("expected exit 1 for invalid type, got %d", code)
	}
}

func TestGCDryRunWithExpired(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Create expired reservation directly in store
	s, _ := store.New(dir)
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "old.go",
		Repo:      "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	code := run("gc", "--dry-run")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestGCExpiredOnly(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, _ := store.New(dir)
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "old.go",
		Repo:      "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	code := run("gc", "--expired-only")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestGCQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("gc", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reserve", "q.go", "--repo", "/tmp/r", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReleaseQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "rq.go", "--repo", "/tmp/r")
	code := run("release", "rq.go", "--repo", "/tmp/r", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReleaseAllQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "ra.go", "--repo", "/tmp/r")
	code := run("release", "--all", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("reserve", "json-res.go", "--repo", "/tmp/r")

	code := run("reservations", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsExpiredJSON(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, _ := store.New(dir)
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "expired-json.go",
		Repo:      "/tmp/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	code := run("reservations", "--expired", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusJSON2(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Register agents with cards and varied states for full JSON coverage
	run("register", "agent-1", "--skills", "go,rust", "--task", "br-42")
	run("register", "agent-2")

	// Backdate agent-2 heartbeat to make it stale
	hbPath := filepath.Join(dir, "agents", "agent-2", "heartbeat")
	old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	os.WriteFile(hbPath, []byte(old+"\n"), 0644)

	// Add reservation and command
	run("reserve", "src/**", "--repo", "/tmp/r", "--agent", "agent-1")
	run("cmd", "session", "/verify", "--agent", "agent-1")

	code := run("status", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestHeartbeatQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code := run("heartbeat", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestCardAllEmpty(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// No agents registered — card --all should show empty
	code := run("card", "--all", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReadQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code := run("read", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestBroadcastNoRecipients(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	// Only self registered — broadcast should succeed but send to 0
	code := run("send", "--broadcast", "hello")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestBroadcastQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "other")

	code := run("send", "--broadcast", "hello", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusNoMeta(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Create agent dir without meta.json and without card.json
	os.MkdirAll(filepath.Join(dir, "agents", "orphan"), 0755)

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestCmdQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("cmd", "target", "/test", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestMetricsQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code := run("metrics", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestMetricsWithStaleThreshold(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code := run("metrics", "--stale", "1s")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsQuietEmpty(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reservations", "--quiet")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSpawnInvalidAgent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("spawn", "--repo", "/tmp", "--agent", "invalid-type", "--prompt", "test")
	if code != 1 {
		t.Errorf("expected 1 for invalid agent type, got %d", code)
	}
}

func TestWatchJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")

	done := make(chan int, 1)
	go func() {
		done <- run("watch", "--agent", "test-agent", "--json")
	}()

	time.Sleep(100 * time.Millisecond)
	run("send", "test-agent", "json watch msg", "--agent", "sender")

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("watch --json failed with code %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch --json timed out")
	}
}

func TestGlobalFlagsOnly(t *testing.T) {
	// Only global flags, no command — should show usage
	code := Run([]string{"relay", "--json"})
	if code != 1 {
		t.Errorf("expected 1 for only global flags, got %d", code)
	}
}

func TestExtractGlobalFlagsMissingValue(t *testing.T) {
	// --agent without value
	gf, remaining := extractGlobalFlags([]string{"--agent"})
	if gf.agent != "" {
		t.Errorf("expected empty agent, got %q", gf.agent)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(remaining))
	}

	// --dir without value
	gf2, remaining2 := extractGlobalFlags([]string{"--dir"})
	if gf2.dir != "" {
		t.Errorf("expected empty dir, got %q", gf2.dir)
	}
	if len(remaining2) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(remaining2))
	}
}

func TestReadHighPriorityDisplay(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "urgent msg", "--agent", "sender", "--priority", "urgent")

	code := run("read")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsWithActiveReservations(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Create active and shared reservations
	run("reserve", "active.go", "--repo", "/tmp/r")
	run("reserve", "shared.go", "--repo", "/tmp/r", "--shared")

	code := run("reservations")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsFilterByAgent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "mine.go", "--repo", "/tmp/r")
	code := run("reservations", "--agent", "nonexistent")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReservationsFilterByRepo(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "repo-filter.go", "--repo", "/tmp/r")
	code := run("reservations", "--repo", "/tmp/no-match")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusWithExpiredReservation(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	// Create expired reservation
	s, _ := store.New(dir)
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "expired.go",
		Repo:      "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveNoPatternAllFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Only flags, no positional pattern arg
	code := run("reserve", "--shared", "--repo", "/tmp/r")
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestReleaseNonexistentQuiet(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("release", "nope.go", "--repo", "/tmp/r", "--quiet")
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestStatusAgentNoHeartbeat(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "ghost")

	// Remove heartbeat file
	os.Remove(filepath.Join(dir, "agents", "ghost", "heartbeat"))

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusJSONWithStaleAndCommands(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "agent-1", "--task", "work")

	// Backdate heartbeat
	hbPath := filepath.Join(dir, "agents", "agent-1", "heartbeat")
	old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	os.WriteFile(hbPath, []byte(old+"\n"), 0644)

	// Add a pending command
	run("cmd", "target", "/verify", "--agent", "agent-1")

	code, out := captureRun(t, "status", "--json")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
	if !json.Valid([]byte(strings.TrimSpace(out))) {
		t.Errorf("status --json should be valid JSON")
	}
}

func TestSendBroadcastWithTypedMessage(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "other-1")
	run("register", "other-2")

	code := run("send", "--broadcast", "status update",
		"--type", "status", "--payload", `{"healthy":true}`)
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestWakeGatewayMethod(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Gateway method without gateway installed — should fail
	code := run("wake", "test", "--method", "gateway")
	if code != 1 {
		t.Errorf("expected 1 (no gateway), got %d", code)
	}
}

func TestReleaseWithoutRepo(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Reserve using cwd (no --repo)
	cwd, _ := os.Getwd()
	run("reserve", "cwd-file.go", "--repo", cwd)

	// Release without --repo — should use cwd
	code := run("release", "cwd-file.go")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveWithoutRepo(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Reserve without --repo — should use cwd
	code := run("reserve", "cwd-reserve.go")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestReserveCheckNoConflict(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// No existing reservations — check should pass
	code := run("reserve", "safe.go", "--repo", "/tmp/r", "--check")
	if code != 0 {
		t.Errorf("expected 0 for no conflict, got %d", code)
	}
}

func TestReservationsWithExpiredSharedDisplay(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	s, _ := store.New(dir)
	// Create expired shared reservation
	s.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "test-agent",
		Pattern:   "shared-expired.go",
		Repo:      "/r",
		Exclusive: false,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	// Show expired
	code := run("reservations", "--expired")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestStatusExpiredReservationDisplay(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	s, _ := store.New(dir)
	// Active reservation
	s.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "test-agent", Pattern: "active.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	// Expired reservation
	s.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "test-agent", Pattern: "expired.go", Repo: "/r2",
		Exclusive: false,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	code := run("status")
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestBroadcastNoMessage(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("send", "--broadcast")
	if code != 1 {
		t.Errorf("expected 1 for broadcast without message, got %d", code)
	}
}

// --- Tests that matter: real agent workflow assertions ---

// TestForceReserveActuallyOverrides verifies that --force removes the existing
// reservation and installs the new one. Without this, agents can't recover
// from stale reservations left by crashed peers.
func TestForceReserveActuallyOverrides(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// agent-1 reserves a pattern
	code := run("reserve", "contested.go", "--repo", "/tmp/r", "--agent", "agent-1")
	if code != 0 {
		t.Fatalf("initial reserve: exit %d", code)
	}

	// agent-2 force-reserves the same pattern
	code = run("reserve", "contested.go", "--repo", "/tmp/r", "--agent", "agent-2", "--force")
	if code != 0 {
		t.Fatalf("force reserve: exit %d", code)
	}

	// Verify the reservation now belongs to agent-2
	s, _ := store.New(dir)
	reservations, err := s.ListReservations()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range reservations {
		if r.Pattern == "contested.go" {
			found = true
			if r.Agent != "agent-2" {
				t.Errorf("reservation should belong to agent-2, got %s", r.Agent)
			}
		}
	}
	if !found {
		t.Error("reservation for contested.go should exist after force")
	}
}

// TestSendReadRoundtripThroughCLI exercises the full CLI send→read path with
// all flags and verifies the message arrives with correct fields via --json.
func TestSendReadRoundtripThroughCLI(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "sender-agent")
	run("register", "reader-agent")

	// Send with all available flags
	code := run("send", "reader-agent", "full roundtrip body",
		"--agent", "sender-agent",
		"--subject", "custom subject",
		"--thread", "br-99",
		"--priority", "urgent",
		"--tag", "deploy,hotfix",
		"--type", "task_result",
		"--payload", `{"status":"ok","exit_code":0}`)
	if code != 0 {
		t.Fatalf("send: exit %d", code)
	}

	// Read as receiver with JSON output
	code, out := captureRun(t, "read", "--agent", "reader-agent", "--json")
	if code != 0 {
		t.Fatalf("read: exit %d", code)
	}

	var msgs []core.Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &msgs); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, out)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.From != "sender-agent" {
		t.Errorf("From: got %q, want sender-agent", m.From)
	}
	if m.Subject != "custom subject" {
		t.Errorf("Subject: got %q, want 'custom subject'", m.Subject)
	}
	if m.Thread != "br-99" {
		t.Errorf("Thread: got %q, want br-99", m.Thread)
	}
	if m.Priority != "urgent" {
		t.Errorf("Priority: got %q, want urgent", m.Priority)
	}
	if m.Type != "task_result" {
		t.Errorf("Type: got %q, want task_result", m.Type)
	}
	// Compare payload semantically (JSON output may be reformatted)
	var gotPayload, wantPayload map[string]interface{}
	json.Unmarshal(m.Payload, &gotPayload)
	json.Unmarshal([]byte(`{"status":"ok","exit_code":0}`), &wantPayload)
	if gotPayload["status"] != wantPayload["status"] || gotPayload["exit_code"] != wantPayload["exit_code"] {
		t.Errorf("Payload mismatch: got %s", m.Payload)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "deploy" || m.Tags[1] != "hotfix" {
		t.Errorf("Tags: got %v, want [deploy hotfix]", m.Tags)
	}
}

// TestBroadcastDeliveryAndSenderExclusion verifies that broadcast messages
// reach all registered agents EXCEPT the sender. This is the foundation
// of multi-agent coordination announcements.
func TestBroadcastDeliveryAndSenderExclusion(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	run("register", "orchestrator")
	run("register", "worker-1")
	run("register", "worker-2")

	code := run("send", "--broadcast", "all hands", "--agent", "orchestrator")
	if code != 0 {
		t.Fatalf("broadcast: exit %d", code)
	}

	s, _ := store.New(dir)
	// Workers should have the message
	for _, name := range []string{"worker-1", "worker-2"} {
		msgs, err := s.ReadInbox(name, store.ReadOpts{})
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(msgs) != 1 || msgs[0].Body != "all hands" {
			t.Errorf("%s should have broadcast message, got %d msgs", name, len(msgs))
		}
	}
	// Sender should NOT have the message
	msgs, _ := s.ReadInbox("orchestrator", store.ReadOpts{})
	if len(msgs) != 0 {
		t.Errorf("sender should not receive own broadcast, got %d msgs", len(msgs))
	}
}

// TestUnreadCursorSurvivesSessions verifies the mark-read cursor persists
// across separate read calls. This is how agents avoid re-processing
// messages they've already seen.
func TestUnreadCursorSurvivesSessions(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")

	// Send 2 messages, read with --mark-read
	run("send", "test-agent", "msg-1", "--agent", "sender")
	run("send", "test-agent", "msg-2", "--agent", "sender")
	code := run("read", "--mark-read")
	if code != 0 {
		t.Fatalf("read --mark-read: exit %d", code)
	}

	// Send 1 more message
	run("send", "test-agent", "msg-3", "--agent", "sender")

	// Read --unread should only show msg-3
	code, out := captureRun(t, "read", "--unread", "--json")
	if code != 0 {
		t.Fatalf("read --unread: exit %d", code)
	}
	var msgs []core.Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &msgs); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(msgs))
	}
	if msgs[0].Body != "msg-3" {
		t.Errorf("expected body=msg-3, got %q", msgs[0].Body)
	}
}

// TestStatusJSONContainsAllExpectedFields verifies the JSON status output
// structure that monitoring tools and orchestrators parse.
func TestStatusJSONContainsAllExpectedFields(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "agent-1", "--skills", "go", "--task", "br-42")

	code, out := captureRun(t, "status", "--json")
	if code != 0 {
		t.Fatalf("status --json: exit %d", code)
	}

	var status struct {
		Agents       []json.RawMessage `json:"agents"`
		Reservations []json.RawMessage `json:"reservations"`
		Commands     []json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &status); err != nil {
		t.Fatalf("parse status JSON: %v", err)
	}
	if len(status.Agents) == 0 {
		t.Error("agents array should not be empty")
	}
	// Verify the agent entry has the expected fields
	var agent struct {
		Name       string   `json:"name"`
		Task       string   `json:"task"`
		Skills     []string `json:"skills"`
		CardStatus string   `json:"card_status"`
		Alive      bool     `json:"alive"`
	}
	if err := json.Unmarshal(status.Agents[0], &agent); err != nil {
		t.Fatalf("parse agent: %v", err)
	}
	if agent.Name != "agent-1" {
		t.Errorf("agent name: got %q, want agent-1", agent.Name)
	}
	if agent.Task != "br-42" {
		t.Errorf("agent task: got %q, want br-42", agent.Task)
	}
	if len(agent.Skills) != 1 || agent.Skills[0] != "go" {
		t.Errorf("agent skills: got %v, want [go]", agent.Skills)
	}
	if !agent.Alive {
		t.Error("freshly registered agent should be alive")
	}
}
