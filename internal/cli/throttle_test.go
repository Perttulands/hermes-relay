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
	dir, cleanup := setup(t)
	defer cleanup()

	setupAllowAllPolicy(t, dir)

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

func TestThrottlePauseExternal(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("throttle", "--pause-external")
	if code != 0 {
		t.Fatalf("throttle --pause-external failed with code %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "throttle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state store.ThrottleState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if !state.PauseExternal {
		t.Fatal("expected pause_external=true")
	}

	auditData, err := os.ReadFile(filepath.Join(dir, "harbour-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(auditData), "\"action\":\"pause_external\"") {
		t.Fatalf("expected harbour audit pause event, got: %q", string(auditData))
	}
}

func TestSendRejectedWhenExternalPaused(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "target")
	run("throttle", "--pause-external")

	code, stderr := captureRunStderr(t, "send", "target", "blocked by pause")
	if code != 1 {
		t.Fatalf("expected send failure when external paused, got %d", code)
	}
	if !strings.Contains(stderr, "external sends are paused") {
		t.Fatalf("expected pause error message, got: %q", stderr)
	}
}

func TestSendAllowedWhenExternalPausedForTrustLevelFour(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "target")

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePolicy(&store.ActivationPolicy{
		Default: "deny",
		Allow: []store.PolicyRule{
			{From: "test-agent", To: "target", TrustLevel: 4},
		},
	}); err != nil {
		t.Fatal(err)
	}

	run("throttle", "--pause-external")
	code := run("send", "target", "native allowed")
	if code != 0 {
		t.Fatalf("expected trust_level=4 send allowed during pause, got %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "native allowed") {
		t.Fatalf("expected message delivered for trust_level=4 sender, got: %q", string(data))
	}
}

func TestThrottleKillExternalDropsPendingWakes(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "target")
	run("throttle", "--pause-external")

	// Queue one pending external wake.
	code := run("send", "target", "pending wake", "--wake")
	if code != 1 {
		t.Fatalf("expected paused external wake to be rejected/queued, got %d", code)
	}

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := s.GetThrottleState()
	if err != nil {
		t.Fatal(err)
	}
	if len(stateBefore.PendingExternalWakes) == 0 {
		t.Fatal("expected pending external wake before kill")
	}

	code = run("throttle", "--kill-external")
	if code != 0 {
		t.Fatalf("throttle --kill-external failed with code %d", code)
	}

	stateAfter, err := s.GetThrottleState()
	if err != nil {
		t.Fatal(err)
	}
	if len(stateAfter.PendingExternalWakes) != 0 {
		t.Fatalf("expected pending external wakes dropped, got %d", len(stateAfter.PendingExternalWakes))
	}

	auditData, err := os.ReadFile(filepath.Join(dir, "harbour-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(auditData), "\"action\":\"kill_external\"") {
		t.Fatalf("expected harbour audit kill event, got: %q", string(auditData))
	}
}
