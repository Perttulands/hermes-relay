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
	Suspended            bool                  `json:"suspended"`
	SuspendedAt          *string               `json:"suspended_at"` // null if not suspended
	SuspendedBy          string                `json:"suspended_by,omitempty"`
	PauseExternal        bool                  `json:"pause_external,omitempty"`
	PauseExternalAt      *string               `json:"pause_external_at,omitempty"`
	PauseExternalBy      string                `json:"pause_external_by,omitempty"`
	Budgets              map[string]int        `json:"budgets,omitempty"`
	PendingExternalWakes []PendingExternalWake `json:"pending_external_wakes,omitempty"`
}

// PendingExternalWake is an external wake request held during pause-external.
type PendingExternalWake struct {
	TS         string `json:"ts"`
	From       string `json:"from"`
	To         string `json:"to"`
	TrustLevel int    `json:"trust_level"`
	ID         string `json:"id"`
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

// IsExternalPaused returns true if pause-external is active.
func (d *Dir) IsExternalPaused() bool {
	state, err := d.GetThrottleState()
	if err != nil {
		return false
	}
	return state.PauseExternal
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

// SetExternalPaused toggles pause-external while preserving other throttle state.
func (d *Dir) SetExternalPaused(paused bool, by string) error {
	state, err := d.GetThrottleState()
	if err != nil {
		state = &ThrottleState{}
	}
	state.PauseExternal = paused
	state.PauseExternalBy = by
	if paused {
		now := nowUTC()
		state.PauseExternalAt = &now
	} else {
		state.PauseExternalAt = nil
		state.PauseExternalBy = ""
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

// QueuePendingExternalWake appends a wake request into throttle state.
func (d *Dir) QueuePendingExternalWake(w PendingExternalWake) error {
	state, err := d.GetThrottleState()
	if err != nil {
		state = &ThrottleState{}
	}
	state.PendingExternalWakes = append(state.PendingExternalWakes, w)
	return atomicWriteJSON(d.throttlePath(), state)
}

// DropPendingExternalWakes drops all queued wakes with trust level below 4.
func (d *Dir) DropPendingExternalWakes() (int, error) {
	state, err := d.GetThrottleState()
	if err != nil {
		state = &ThrottleState{}
	}
	if len(state.PendingExternalWakes) == 0 {
		return 0, atomicWriteJSON(d.throttlePath(), state)
	}
	kept := make([]PendingExternalWake, 0, len(state.PendingExternalWakes))
	dropped := 0
	for _, w := range state.PendingExternalWakes {
		if w.TrustLevel < 4 {
			dropped++
			continue
		}
		kept = append(kept, w)
	}
	state.PendingExternalWakes = kept
	if err := atomicWriteJSON(d.throttlePath(), state); err != nil {
		return 0, err
	}
	return dropped, nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
