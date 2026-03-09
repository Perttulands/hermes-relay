package store

import (
	"testing"

	"github.com/Perttulands/hermes-relay/internal/core"
)

func TestLoadChainNotFound(t *testing.T) {
	d := tempDir(t)
	chain, err := d.LoadChain("nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain != nil {
		t.Fatalf("expected nil for missing chain, got %+v", chain)
	}
}

func TestRecordHopCreatesNewChain(t *testing.T) {
	d := tempDir(t)
	chainID := core.NewChainID()

	chain, err := d.RecordHop(chainID, "hestia", "hermes", 3)
	if err != nil {
		t.Fatalf("RecordHop: %v", err)
	}
	if chain.ID != chainID {
		t.Errorf("expected chain ID %s, got %s", chainID, chain.ID)
	}
	if chain.RootSender != "hestia" {
		t.Errorf("expected root sender hestia, got %s", chain.RootSender)
	}
	if chain.Depth != 1 {
		t.Errorf("expected depth 1, got %d", chain.Depth)
	}
	if chain.MaxDepth != 3 {
		t.Errorf("expected max depth 3, got %d", chain.MaxDepth)
	}
	if len(chain.Hops) != 1 {
		t.Fatalf("expected 1 hop, got %d", len(chain.Hops))
	}
	if chain.Hops[0].From != "hestia" || chain.Hops[0].To != "hermes" {
		t.Errorf("unexpected hop: %+v", chain.Hops[0])
	}
	if chain.Hops[0].Depth != 1 {
		t.Errorf("expected hop depth 1, got %d", chain.Hops[0].Depth)
	}
}

func TestRecordHopPropagatesExistingChain(t *testing.T) {
	d := tempDir(t)
	chainID := core.NewChainID()

	// First hop
	_, err := d.RecordHop(chainID, "hestia", "hermes", 5)
	if err != nil {
		t.Fatalf("first RecordHop: %v", err)
	}

	// Second hop
	chain, err := d.RecordHop(chainID, "hermes", "athena", 5)
	if err != nil {
		t.Fatalf("second RecordHop: %v", err)
	}
	if chain.Depth != 2 {
		t.Errorf("expected depth 2, got %d", chain.Depth)
	}
	if chain.RootSender != "hestia" {
		t.Errorf("root sender should remain hestia, got %s", chain.RootSender)
	}
	if len(chain.Hops) != 2 {
		t.Fatalf("expected 2 hops, got %d", len(chain.Hops))
	}
	if chain.Hops[1].From != "hermes" || chain.Hops[1].To != "athena" {
		t.Errorf("unexpected second hop: %+v", chain.Hops[1])
	}
}

func TestRecordHopDepthIncrements(t *testing.T) {
	d := tempDir(t)
	chainID := core.NewChainID()

	for i := 1; i <= 5; i++ {
		chain, err := d.RecordHop(chainID, "a", "b", 10)
		if err != nil {
			t.Fatalf("hop %d: %v", i, err)
		}
		if chain.Depth != i {
			t.Errorf("hop %d: expected depth %d, got %d", i, i, chain.Depth)
		}
	}
}

func TestChainDepthExceeded(t *testing.T) {
	d := tempDir(t)
	chainID := core.NewChainID()

	// Fill to max depth 2
	for i := 0; i < 2; i++ {
		_, err := d.RecordHop(chainID, "a", "b", 2)
		if err != nil {
			t.Fatalf("hop %d: %v", i+1, err)
		}
	}

	// Third hop exceeds max depth
	chain, err := d.RecordHop(chainID, "b", "c", 2)
	if err != nil {
		t.Fatalf("exceeding hop: %v", err)
	}
	if chain.Depth != 3 {
		t.Errorf("expected depth 3, got %d", chain.Depth)
	}
	if chain.Depth <= chain.MaxDepth {
		t.Errorf("expected depth %d > max_depth %d", chain.Depth, chain.MaxDepth)
	}
}

func TestSaveAndLoadChainRoundtrip(t *testing.T) {
	d := tempDir(t)
	chainID := core.NewChainID()

	original := &core.ChainState{
		ID:         chainID,
		RootSender: "hestia",
		RootTS:     "2026-03-07T08:30:00Z",
		Depth:      2,
		MaxDepth:   3,
		Suspended:  true,
		Hops: []core.ChainHop{
			{From: "hestia", To: "hermes", TS: "2026-03-07T08:30:00Z", Depth: 1},
			{From: "hermes", To: "athena", TS: "2026-03-07T08:31:00Z", Depth: 2},
		},
	}

	if err := d.SaveChain(original); err != nil {
		t.Fatalf("SaveChain: %v", err)
	}

	loaded, err := d.LoadChain(chainID)
	if err != nil {
		t.Fatalf("LoadChain: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil chain")
	}
	if loaded.ID != original.ID {
		t.Errorf("ID mismatch: %s vs %s", loaded.ID, original.ID)
	}
	if loaded.Suspended != original.Suspended {
		t.Errorf("Suspended mismatch: %v vs %v", loaded.Suspended, original.Suspended)
	}
	if len(loaded.Hops) != len(original.Hops) {
		t.Errorf("Hops length mismatch: %d vs %d", len(loaded.Hops), len(original.Hops))
	}
}

func TestNewChainIDFormat(t *testing.T) {
	id := core.NewChainID()
	if len(id) != 36 {
		t.Errorf("expected UUID length 36, got %d: %s", len(id), id)
	}
	// Check UUID v4 format: 8-4-4-4-12
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("invalid UUID format: %s", id)
	}
}

func TestNewChainIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := core.NewChainID()
		if seen[id] {
			t.Fatalf("duplicate chain ID: %s", id)
		}
		seen[id] = true
	}
}
