package core

import (
	"crypto/rand"
	"fmt"
)

// ChainHop records a single hop in a wake chain.
type ChainHop struct {
	From  string `json:"from"`
	To    string `json:"to"`
	TS    string `json:"ts"`
	Depth int    `json:"depth"`
}

// ChainState tracks the state of a wake chain (a sequence of agent-to-agent activations).
type ChainState struct {
	ID         string     `json:"id"`
	RootSender string     `json:"root_sender"`
	RootTS     string     `json:"root_ts"`
	Depth      int        `json:"depth"`
	MaxDepth   int        `json:"max_depth"`
	Suspended  bool       `json:"suspended"`
	Hops       []ChainHop `json:"hops"`
}

// DefaultMaxDepth is the default maximum wake chain depth.
const DefaultMaxDepth = 3

// NewChainID generates a new UUID v4 for chain identification.
func NewChainID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
