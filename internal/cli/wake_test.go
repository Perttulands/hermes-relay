package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/hermes-relay/internal/core"
)

func TestSendWakeInjectsViaOpenClaw(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Register target with gateway_url
	run("register", "target-agent", "--gateway-url", "ws://localhost:4000/", "--gateway-token", "tok123")

	run("register", "test-agent")

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		// openclaw succeeds
		return exec.Command("true")
	})

	code := run("send", "target-agent", "do the thing", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should have called openclaw system event
	if len(calls) == 0 {
		t.Fatal("expected openclaw call, got none")
	}
	call := calls[0]
	if !strings.Contains(call, "openclaw system event") {
		t.Errorf("expected openclaw system event call, got: %s", call)
	}
	if !strings.Contains(call, "--url ws://localhost:4000/") {
		t.Errorf("expected --url flag, got: %s", call)
	}
	if !strings.Contains(call, "--token tok123") {
		t.Errorf("expected --token flag, got: %s", call)
	}
	if !strings.Contains(call, "--text do the thing") {
		t.Errorf("expected --text flag, got: %s", call)
	}
	if !strings.Contains(call, "--mode now") {
		t.Errorf("expected --mode now, got: %s", call)
	}

	// Should NOT have called systemctl (openclaw succeeded)
	for _, c := range calls {
		if strings.Contains(c, "systemctl") {
			t.Errorf("should not fall back to systemctl when openclaw succeeds, got: %s", c)
		}
	}
}

func TestSendWakeFallsBackWhenNoGatewayURL(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Register target WITHOUT gateway_url
	run("register", "target-agent")
	run("register", "test-agent")

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		// systemctl fails, gateway fails — both expected in test
		return exec.Command("false")
	})

	// Should not crash, falls through to existing logic
	run("send", "target-agent", "hello", "--wake")

	// Should NOT have called openclaw (no gateway_url)
	for _, c := range calls {
		if strings.HasPrefix(c, "openclaw") {
			t.Errorf("should not call openclaw when no gateway_url, got: %s", c)
		}
	}

	// Should have tried systemctl
	foundSystemctl := false
	for _, c := range calls {
		if strings.Contains(c, "systemctl") {
			foundSystemctl = true
			break
		}
	}
	if !foundSystemctl {
		t.Errorf("expected systemctl fallback call, got: %v", calls)
	}
}

func TestSendWakeFallsBackWhenOpenClawFails(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Register target with gateway_url
	run("register", "target-agent", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	callIndex := 0
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		callIndex++
		if strings.Contains(name, "openclaw") || (len(args) > 0 && args[0] == "system") {
			// openclaw fails
			return exec.Command("false")
		}
		// systemctl also fails (expected in test)
		return exec.Command("false")
	})

	// Should not crash — falls through to systemctl
	run("send", "target-agent", "hello", "--wake")
}

func TestRegisterGatewayFlagsWrittenToMeta(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("register", "gw-agent",
		"--gateway-url", "ws://localhost:4000/",
		"--gateway-token", "secret-tok",
		"--session-key", "agent:main:main",
	)
	if code != 0 {
		t.Fatalf("register failed with code %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "gw-agent", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta core.AgentMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.GatewayURL != "ws://localhost:4000/" {
		t.Errorf("expected gateway_url=ws://localhost:4000/, got %s", meta.GatewayURL)
	}
	if meta.GatewayToken != "secret-tok" {
		t.Errorf("expected gateway_token=secret-tok, got %s", meta.GatewayToken)
	}
	if meta.SessionKey != "agent:main:main" {
		t.Errorf("expected session_key=agent:main:main, got %s", meta.SessionKey)
	}
}

func TestSendWakeNoTokenWhenEmpty(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Register target with gateway_url but NO token
	run("register", "target-agent", "--gateway-url", "ws://localhost:5000/")
	run("register", "test-agent")

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target-agent", "msg", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	if len(calls) == 0 {
		t.Fatal("expected openclaw call")
	}
	if strings.Contains(calls[0], "--token") {
		t.Errorf("should not include --token when gateway_token is empty, got: %s", calls[0])
	}
}
