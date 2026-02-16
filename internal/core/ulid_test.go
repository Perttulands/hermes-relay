package core

import "testing"

func TestNewULIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewULID()
		if seen[id] {
			t.Fatalf("duplicate ULID: %s", id)
		}
		seen[id] = true
	}
}

func TestNewULIDSorted(t *testing.T) {
	prev := ""
	for i := 0; i < 100; i++ {
		id := NewULID()
		if id <= prev {
			t.Fatalf("ULID not monotonically increasing: %s <= %s", id, prev)
		}
		prev = id
	}
}

func TestNewULIDLength(t *testing.T) {
	id := NewULID()
	if len(id) != 26 {
		t.Errorf("expected ULID length 26, got %d", len(id))
	}
}
