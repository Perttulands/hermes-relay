package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ThrottleState represents the city-wide throttle configuration.
type ThrottleState struct {
	Suspended   bool           `json:"suspended"`
	SuspendedAt *string        `json:"suspended_at"` // null if not suspended
	SuspendedBy string         `json:"suspended_by,omitempty"`
	Budgets     map[string]int `json:"budgets,omitempty"`
}

func (d *Dir) throttlePath() string {
	return filepath.Join(d.Root, "throttle.json")
}

// IsThrottled returns true if the city-wide throttle is active.
// Returns false if the throttle file does not exist or cannot be read.
func (d *Dir) IsThrottled() bool {
	state, err := d.GetThrottleState()
	if err != nil {
		return false
	}
	return state.Suspended
}

// GetThrottleState reads the full throttle state from throttle.json.
// Returns a zero-value ThrottleState if the file does not exist.
func (d *Dir) GetThrottleState() (*ThrottleState, error) {
	data, err := os.ReadFile(d.throttlePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &ThrottleState{}, nil
		}
		return nil, fmt.Errorf("read throttle state: %w", err)
	}
	var state ThrottleState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse throttle state: %w", err)
	}
	return &state, nil
}

// SetThrottled writes the throttle state, preserving existing budgets.
func (d *Dir) SetThrottled(suspended bool, by string) error {
	state, err := d.GetThrottleState()
	if err != nil {
		state = &ThrottleState{}
	}
	state.Suspended = suspended
	state.SuspendedBy = by
	if suspended {
		now := nowUTC()
		state.SuspendedAt = &now
	} else {
		state.SuspendedAt = nil
		state.SuspendedBy = ""
	}
	return atomicWriteJSON(d.throttlePath(), state)
}

// SetBudget updates the per-agent wake budget in throttle.json.
func (d *Dir) SetBudget(agent string, n int) error {
	state, err := d.GetThrottleState()
	if err != nil {
		state = &ThrottleState{}
	}
	if state.Budgets == nil {
		state.Budgets = make(map[string]int)
	}
	state.Budgets[agent] = n
	return atomicWriteJSON(d.throttlePath(), state)
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
