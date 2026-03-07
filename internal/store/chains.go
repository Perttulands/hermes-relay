package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
)

// chainsDir returns the path to the chains directory.
func (d *Dir) chainsDir() string {
	return filepath.Join(d.Root, "chains")
}

// chainPath returns the path to a specific chain state file.
func (d *Dir) chainPath(id string) string {
	return filepath.Join(d.chainsDir(), id+".json")
}

// LoadChain reads a chain state from disk. Returns nil, nil if not found (new chain).
func (d *Dir) LoadChain(id string) (*core.ChainState, error) {
	data, err := os.ReadFile(d.chainPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chain %s: %w", id, err)
	}
	var state core.ChainState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse chain %s: %w", id, err)
	}
	return &state, nil
}

// SaveChain writes a chain state to disk atomically.
func (d *Dir) SaveChain(state *core.ChainState) error {
	dir := d.chainsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create chains dir: %w", err)
	}
	return atomicWriteJSON(d.chainPath(state.ID), state)
}

// RecordHop atomically loads the chain, appends a hop, increments depth, and saves.
// If the chain does not exist, a new one is created with the given parameters.
func (d *Dir) RecordHop(chainID, from, to string, maxDepth int) (*core.ChainState, error) {
	dir := d.chainsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create chains dir: %w", err)
	}

	lockPath := d.chainPath(chainID) + ".lock"

	state, err := d.LoadChain(chainID)
	if err != nil {
		return nil, fmt.Errorf("record hop: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	if state == nil {
		state = &core.ChainState{
			ID:         chainID,
			RootSender: from,
			RootTS:     now,
			Depth:      0,
			MaxDepth:   maxDepth,
			Hops:       []core.ChainHop{},
		}
	}

	// Update max_depth if caller provides a higher value
	if maxDepth > state.MaxDepth {
		state.MaxDepth = maxDepth
	}

	state.Depth++
	state.Hops = append(state.Hops, core.ChainHop{
		From:  from,
		To:    to,
		TS:    now,
		Depth: state.Depth,
	})

	if err := d.SaveChain(state); err != nil {
		return nil, fmt.Errorf("record hop save: %w", err)
	}

	// Clean up lock file best-effort
	_ = os.Remove(lockPath)

	return state, nil
}
