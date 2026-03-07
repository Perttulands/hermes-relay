package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
	"github.com/Perttulands/hermes-relay/internal/store"
)

func TestRegisterWithBudget(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("register", "budget-agent", "--budget", "10")
	if code != 0 {
		t.Fatalf("register --budget failed with code %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "budget-agent", "card.json"))
	if err != nil {
		t.Fatal(err)
	}
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.BudgetLimit != 10 {
		t.Errorf("expected budget_limit=10, got %d", card.BudgetLimit)
	}
}

func TestRegisterWithCooldown(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("register", "cd-agent", "--cooldown", "10m")
	if code != 0 {
		t.Fatalf("register --cooldown failed with code %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "cd-agent", "card.json"))
	if err != nil {
		t.Fatal(err)
	}
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.CooldownSecs != 600 {
		t.Errorf("expected cooldown_secs=600, got %d", card.CooldownSecs)
	}
}

func TestRegisterInvalidBudget(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "bad-agent", "--budget", "abc")
	if code != 1 {
		t.Errorf("expected exit 1 for invalid budget, got %d", code)
	}
}

func TestRegisterInvalidCooldown(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "bad-agent", "--cooldown", "notaduration")
	if code != 1 {
		t.Errorf("expected exit 1 for invalid cooldown, got %d", code)
	}
}

func TestSendWakeBudgetExhaustedSkipsInjection(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Register target with gateway URL
	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	// Exhaust budget by writing budget.json at limit
	s, _ := store.New(dir)
	budgetState := struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
		Limit int    `json:"limit"`
	}{
		Date:  time.Now().Format("2006-01-02"), // close enough for test
		Count: 20,
		Limit: 20,
	}
	budgetData, _ := json.MarshalIndent(budgetState, "", "  ")
	os.WriteFile(filepath.Join(s.AgentDir("target"), "budget.json"), budgetData, 0644)

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "hello", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should NOT have called openclaw (budget exhausted)
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			t.Errorf("should not call openclaw when budget exhausted, got: %s", c)
		}
	}

	// Message should still be delivered
	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		t.Fatal("inbox should exist")
	}
	if !strings.Contains(string(data), "hello") {
		t.Error("message should still be in inbox")
	}
}

func TestSendWakeCooldownSkipsInjection(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	// Write cooldown with recent last_wake
	s, _ := store.New(dir)
	cdState := struct {
		LastWake   string `json:"last_wake"`
		MinSeconds int    `json:"min_seconds"`
	}{
		LastWake:   time.Now().UTC().Format(time.RFC3339),
		MinSeconds: 300,
	}
	cdData, _ := json.MarshalIndent(cdState, "", "  ")
	os.WriteFile(filepath.Join(s.AgentDir("target"), "cooldown.json"), cdData, 0644)

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "hello cooldown", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should NOT have called openclaw (cooling down)
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			t.Errorf("should not call openclaw when cooling down, got: %s", c)
		}
	}

	// Message should still be delivered
	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		t.Fatal("inbox should exist")
	}
	if !strings.Contains(string(data), "hello cooldown") {
		t.Error("message should still be in inbox")
	}
}

func TestSendWakeAllowedWhenBudgetAndCooldownOk(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "go ahead", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should have called openclaw
	foundOpenclaw := false
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			foundOpenclaw = true
			break
		}
	}
	if !foundOpenclaw {
		t.Errorf("expected openclaw call when budget/cooldown ok, got: %v", calls)
	}
}
