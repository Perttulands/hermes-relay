package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
)

func registerAgent(t *testing.T, d *Dir, name string) {
	t.Helper()
	meta := core.AgentMeta{Name: name, RegisteredAt: time.Now().UTC().Format(time.RFC3339)}
	if err := d.Register(meta); err != nil {
		t.Fatal(err)
	}
	card := core.AgentCard{Name: name, RegisteredAt: meta.RegisteredAt}
	if err := d.WriteCard(card); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetAllowsWhenUnderLimit(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-a")

	allowed, err := d.CheckAndIncrementBudget("agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("expected budget to allow wake")
	}

	// Read back and verify count incremented
	state, err := d.readBudget("agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 1 {
		t.Errorf("expected count=1, got %d", state.Count)
	}
}

func TestBudgetBlocksWhenExhausted(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-b")

	// Write a budget that's already at limit
	state := &BudgetState{
		Date:  helsinkiDate(),
		Count: 20,
		Limit: 20,
	}
	atomicWriteJSON(d.budgetPath("agent-b"), state)

	allowed, err := d.CheckAndIncrementBudget("agent-b")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected budget to block wake when exhausted")
	}
}

func TestBudgetResetsOnDateChange(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-c")

	// Write a budget from yesterday that was exhausted
	state := &BudgetState{
		Date:  "2020-01-01",
		Count: 20,
		Limit: 20,
	}
	atomicWriteJSON(d.budgetPath("agent-c"), state)

	allowed, err := d.CheckAndIncrementBudget("agent-c")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("expected budget to reset on date change and allow wake")
	}

	// Verify it was reset
	newState, _ := d.readBudget("agent-c")
	if newState.Count != 1 {
		t.Errorf("expected count=1 after reset, got %d", newState.Count)
	}
	if newState.Date != helsinkiDate() {
		t.Errorf("expected date=%s, got %s", helsinkiDate(), newState.Date)
	}
}

func TestCooldownBlocksWhenTooRecent(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-d")

	// Set last_wake to now
	state := &CooldownState{
		LastWake:   time.Now().UTC().Format(time.RFC3339),
		MinSeconds: 300,
	}
	atomicWriteJSON(d.cooldownPath("agent-d"), state)

	cooling, err := d.IsCoolingDown("agent-d")
	if err != nil {
		t.Fatal(err)
	}
	if !cooling {
		t.Error("expected cooldown to be active")
	}
}

func TestCooldownAllowsAfterElapsed(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-e")

	// Set last_wake to 10 minutes ago with 5 min cooldown
	state := &CooldownState{
		LastWake:   time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		MinSeconds: 300,
	}
	atomicWriteJSON(d.cooldownPath("agent-e"), state)

	cooling, err := d.IsCoolingDown("agent-e")
	if err != nil {
		t.Fatal(err)
	}
	if cooling {
		t.Error("expected cooldown to have elapsed")
	}
}

func TestCooldownDefaultWhenNoFile(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-f")

	cooling, err := d.IsCoolingDown("agent-f")
	if err != nil {
		t.Fatal(err)
	}
	if cooling {
		t.Error("expected no cooldown when no prior wake")
	}
}

func TestUpdateCooldown(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-g")

	if err := d.UpdateCooldown("agent-g"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(d.cooldownPath("agent-g"))
	if err != nil {
		t.Fatal(err)
	}
	var state CooldownState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.LastWake == "" {
		t.Error("expected last_wake to be set")
	}
	if state.MinSeconds != DefaultCooldownSecs {
		t.Errorf("expected min_seconds=%d, got %d", DefaultCooldownSecs, state.MinSeconds)
	}
}

func TestBudgetUsesCardLimit(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-h")

	// Update card with custom budget limit
	card, _ := d.ReadCard("agent-h")
	card.BudgetLimit = 5
	d.WriteCard(card)

	// Exhaust the custom limit
	for i := 0; i < 5; i++ {
		allowed, err := d.CheckAndIncrementBudget("agent-h")
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("expected wake %d to be allowed", i+1)
		}
	}

	// 6th should be blocked
	allowed, err := d.CheckAndIncrementBudget("agent-h")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected budget to be exhausted at custom limit of 5")
	}
}

func TestCooldownUsesCardSecs(t *testing.T) {
	d := tempDir(t)
	registerAgent(t, d, "agent-i")

	// Set card cooldown to 600s
	card, _ := d.ReadCard("agent-i")
	card.CooldownSecs = 600
	d.WriteCard(card)

	// Set last wake to 8 minutes ago — should still be cooling (600s = 10min)
	state := &CooldownState{
		LastWake:   time.Now().Add(-8 * time.Minute).UTC().Format(time.RFC3339),
		MinSeconds: 600,
	}
	atomicWriteJSON(d.cooldownPath("agent-i"), state)

	cooling, err := d.IsCoolingDown("agent-i")
	if err != nil {
		t.Fatal(err)
	}
	if !cooling {
		t.Error("expected cooldown to still be active at 8 min with 600s limit")
	}
}
