package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultBudgetLimit  = 20
	DefaultCooldownSecs = 300
)

// helsinkiDate returns today's date string in Europe/Helsinki timezone.
func helsinkiDate() string {
	loc, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		// Fallback: UTC+2 (EET) if tzdata not available
		loc = time.FixedZone("EET", 2*60*60)
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// BudgetState tracks daily wake activations for an agent.
type BudgetState struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Limit int    `json:"limit"`
}

// CooldownState tracks the last wake time for an agent.
type CooldownState struct {
	LastWake   string `json:"last_wake"`
	MinSeconds int    `json:"min_seconds"`
}

func (d *Dir) budgetPath(agent string) string {
	return filepath.Join(d.AgentDir(agent), "budget.json")
}

func (d *Dir) cooldownPath(agent string) string {
	return filepath.Join(d.AgentDir(agent), "cooldown.json")
}

// readBudget reads the budget state for an agent.
// Returns a fresh state with defaults if file doesn't exist.
func (d *Dir) readBudget(agent string) (*BudgetState, error) {
	data, err := os.ReadFile(d.budgetPath(agent))
	if err != nil {
		if os.IsNotExist(err) {
			limit := d.agentBudgetLimit(agent)
			return &BudgetState{Date: helsinkiDate(), Count: 0, Limit: limit}, nil
		}
		return nil, fmt.Errorf("read budget for %s: %w", agent, err)
	}
	var state BudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse budget for %s: %w", agent, err)
	}
	return &state, nil
}

// readCooldown reads the cooldown state for an agent.
func (d *Dir) readCooldown(agent string) (*CooldownState, error) {
	data, err := os.ReadFile(d.cooldownPath(agent))
	if err != nil {
		if os.IsNotExist(err) {
			secs := d.agentCooldownSecs(agent)
			return &CooldownState{MinSeconds: secs}, nil
		}
		return nil, fmt.Errorf("read cooldown for %s: %w", agent, err)
	}
	var state CooldownState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse cooldown for %s: %w", agent, err)
	}
	return &state, nil
}

// agentBudgetLimit returns the budget limit for an agent from its card,
// falling back to DefaultBudgetLimit.
func (d *Dir) agentBudgetLimit(agent string) int {
	card, err := d.ReadCard(agent)
	if err == nil && card.BudgetLimit > 0 {
		return card.BudgetLimit
	}
	return DefaultBudgetLimit
}

// agentCooldownSecs returns the cooldown seconds for an agent from its card,
// falling back to DefaultCooldownSecs.
func (d *Dir) agentCooldownSecs(agent string) int {
	card, err := d.ReadCard(agent)
	if err == nil && card.CooldownSecs > 0 {
		return card.CooldownSecs
	}
	return DefaultCooldownSecs
}

// CheckAndIncrementBudget checks whether the agent has budget remaining today.
// If allowed, it increments the counter and persists. Date rollover resets count.
func (d *Dir) CheckAndIncrementBudget(agent string) (bool, error) {
	state, err := d.readBudget(agent)
	if err != nil {
		return false, err
	}

	today := helsinkiDate()
	if state.Date != today {
		// Date rollover: reset
		state.Date = today
		state.Count = 0
		state.Limit = d.agentBudgetLimit(agent)
	}

	if state.Count >= state.Limit {
		return false, nil
	}

	state.Count++
	if err := atomicWriteJSON(d.budgetPath(agent), state); err != nil {
		return false, fmt.Errorf("write budget for %s: %w", agent, err)
	}
	return true, nil
}

// IsCoolingDown returns true if the agent's cooldown period has not elapsed.
func (d *Dir) IsCoolingDown(agent string) (bool, error) {
	state, err := d.readCooldown(agent)
	if err != nil {
		return false, err
	}
	if state.LastWake == "" {
		return false, nil
	}
	last, err := time.Parse(time.RFC3339, state.LastWake)
	if err != nil {
		return false, fmt.Errorf("parse last_wake for %s: %w", agent, err)
	}
	minSecs := state.MinSeconds
	if minSecs <= 0 {
		minSecs = d.agentCooldownSecs(agent)
	}
	return time.Since(last) < time.Duration(minSecs)*time.Second, nil
}

// UpdateCooldown records a wake timestamp for the agent.
func (d *Dir) UpdateCooldown(agent string) error {
	secs := d.agentCooldownSecs(agent)
	state := &CooldownState{
		LastWake:   time.Now().UTC().Format(time.RFC3339),
		MinSeconds: secs,
	}
	return atomicWriteJSON(d.cooldownPath(agent), state)
}
