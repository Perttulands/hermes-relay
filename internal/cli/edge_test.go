package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/relay/internal/core"
	"github.com/Perttulands/relay/internal/store"
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
