package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/hermes-relay/internal/store"
)

func TestThrottleSuspendAll(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("throttle", "--suspend-all")
	if code != 0 {
		t.Fatalf("throttle --suspend-all failed with code %d", code)
	}

	// Verify throttle.json
	data, err := os.ReadFile(filepath.Join(dir, "throttle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state store.ThrottleState
	json.Unmarshal(data, &state)
	if !state.Suspended {
		t.Error("expected suspended=true")
	}
	if state.SuspendedAt == nil {
		t.Error("expected suspended_at to be set")
	}
}

func TestThrottleResume(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("throttle", "--suspend-all")
	code := run("throttle", "--resume")
	if code != 0 {
		t.Fatalf("throttle --resume failed with code %d", code)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "throttle.json"))
	var state store.ThrottleState
	json.Unmarshal(data, &state)
	if state.Suspended {
		t.Error("expected suspended=false after resume")
	}
}

func TestThrottleStatus(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Status when not suspended
	code, out := captureRun(t, "throttle", "--status")
	if code != 0 {
		t.Fatalf("throttle --status failed with code %d", code)
	}
	if !strings.Contains(out, "normal operation") {
		t.Errorf("expected 'normal operation', got: %q", out)
	}

	// Suspend then check status
	run("throttle", "--suspend-all")
	code, out = captureRun(t, "throttle", "--status")
	if code != 0 {
		t.Fatalf("throttle --status failed with code %d", code)
	}
	if !strings.Contains(out, "SUSPENDED") {
		t.Errorf("expected 'SUSPENDED', got: %q", out)
	}
}

func TestThrottleStatusJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("throttle", "--suspend-all")
	code, out := captureRun(t, "throttle", "--status", "--json")
	if code != 0 {
		t.Fatalf("throttle --status --json failed with code %d", code)
	}
	out = strings.TrimSpace(out)
	if !json.Valid([]byte(out)) {
		t.Errorf("not valid JSON: %q", out)
	}
	var state store.ThrottleState
	json.Unmarshal([]byte(out), &state)
	if !state.Suspended {
		t.Error("expected suspended=true in JSON output")
	}
}

func TestThrottleSetBudget(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("throttle", "--set-budget", "hermes", "10")
	if code != 0 {
		t.Fatalf("throttle --set-budget failed with code %d", code)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "throttle.json"))
	var state store.ThrottleState
	json.Unmarshal(data, &state)
	if state.Budgets["hermes"] != 10 {
		t.Errorf("expected hermes=10, got %d", state.Budgets["hermes"])
	}
}

func TestThrottleSetBudgetMissingArgs(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("throttle", "--set-budget", "hermes")
	if code != 1 {
		t.Errorf("expected exit 1 for missing budget args, got %d", code)
	}
}

func TestThrottleNoFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("throttle")
	if code != 1 {
		t.Errorf("expected exit 1 for no flags, got %d", code)
	}
}

func TestSendWakeSkipsWhenThrottled(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "target")

	// Suspend wakes
	run("throttle", "--suspend-all")

	// Mock exec so we can detect if wake is attempted
	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		return exec.Command("bash", "-lc", "exit 0")
	})

	code := run("send", "target", "hello", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// No exec calls should have been made for wake
	if len(calls) != 0 {
		t.Errorf("expected no wake exec calls when throttled, got %v", calls)
	}

	// Message should still be delivered
	dir := os.Getenv("RELAY_DIR")
	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Error("message should still be delivered to inbox")
	}
}

func TestSendWakeWorksWhenNotThrottled(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "target")

	// No throttle active
	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		return exec.Command("bash", "-lc", "exit 0")
	})

	code := run("send", "target", "hello", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should have attempted wake (systemctl or openclaw)
	if len(calls) == 0 {
		t.Error("expected wake exec calls when not throttled")
	}
}
