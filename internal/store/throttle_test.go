package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestIsThrottledDefault(t *testing.T) {
	d := tempDir(t)
	// No throttle.json — should return false
	if d.IsThrottled() {
		t.Error("expected false when throttle.json missing")
	}
}

func TestSetThrottledAndIsThrottled(t *testing.T) {
	d := tempDir(t)

	if err := d.SetThrottled(true, "hestia"); err != nil {
		t.Fatal(err)
	}
	if !d.IsThrottled() {
		t.Error("expected true after suspend")
	}

	state, err := d.GetThrottleState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Suspended {
		t.Error("expected suspended=true")
	}
	if state.SuspendedBy != "hestia" {
		t.Errorf("expected suspended_by=hestia, got %s", state.SuspendedBy)
	}
	if state.SuspendedAt == nil {
		t.Error("expected suspended_at to be set")
	}
}

func TestResumeThrottle(t *testing.T) {
	d := tempDir(t)

	d.SetThrottled(true, "hestia")
	if err := d.SetThrottled(false, ""); err != nil {
		t.Fatal(err)
	}
	if d.IsThrottled() {
		t.Error("expected false after resume")
	}

	state, _ := d.GetThrottleState()
	if state.Suspended {
		t.Error("expected suspended=false")
	}
	if state.SuspendedAt != nil {
		t.Error("expected suspended_at to be nil")
	}
	if state.SuspendedBy != "" {
		t.Errorf("expected empty suspended_by, got %s", state.SuspendedBy)
	}
}

func TestSetBudget(t *testing.T) {
	d := tempDir(t)

	if err := d.SetBudget("hermes", 10); err != nil {
		t.Fatal(err)
	}
	if err := d.SetBudget("athena", 20); err != nil {
		t.Fatal(err)
	}

	state, err := d.GetThrottleState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Budgets["hermes"] != 10 {
		t.Errorf("expected hermes=10, got %d", state.Budgets["hermes"])
	}
	if state.Budgets["athena"] != 20 {
		t.Errorf("expected athena=20, got %d", state.Budgets["athena"])
	}
}

func TestSetBudgetPreservesSuspendState(t *testing.T) {
	d := tempDir(t)

	d.SetThrottled(true, "hestia")
	if err := d.SetBudget("hermes", 5); err != nil {
		t.Fatal(err)
	}

	state, _ := d.GetThrottleState()
	if !state.Suspended {
		t.Error("expected suspended to be preserved")
	}
	if state.Budgets["hermes"] != 5 {
		t.Errorf("expected hermes=5, got %d", state.Budgets["hermes"])
	}
}

func TestGetThrottleStateMalformedJSON(t *testing.T) {
	d := tempDir(t)
	os.WriteFile(d.throttlePath(), []byte("{broken"), 0644)

	_, err := d.GetThrottleState()
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestThrottleJSONShape(t *testing.T) {
	d := tempDir(t)
	d.SetThrottled(true, "hestia")
	d.SetBudget("hermes", 10)

	data, err := os.ReadFile(d.throttlePath())
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["suspended"] != true {
		t.Errorf("expected suspended=true in JSON, got %v", raw["suspended"])
	}
	if raw["suspended_by"] != "hestia" {
		t.Errorf("expected suspended_by=hestia, got %v", raw["suspended_by"])
	}
	if raw["suspended_at"] == nil {
		t.Error("expected suspended_at to be set")
	}
	budgets, ok := raw["budgets"].(map[string]interface{})
	if !ok {
		t.Fatal("expected budgets map")
	}
	if budgets["hermes"] != float64(10) {
		t.Errorf("expected hermes=10, got %v", budgets["hermes"])
	}
}
