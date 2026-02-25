package core

import (
	"encoding/json"
	"time"
)

// Message types
const (
	TypeTaskResult = "task_result"
	TypeRequest    = "request"
	TypeAlert      = "alert"
	TypeStatus     = "status"
	TypeChat       = "chat"
)

// ValidTypes is the set of recognized message types.
var ValidTypes = map[string]bool{
	TypeTaskResult: true,
	TypeRequest:    true,
	TypeAlert:      true,
	TypeStatus:     true,
	TypeChat:       true,
}

// Message is a single NDJSON line in an agent's inbox.
type Message struct {
	ID       string          `json:"id"`
	TS       string          `json:"ts"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Subject  string          `json:"subject,omitempty"`
	Body     string          `json:"body"`
	Thread   string          `json:"thread,omitempty"`
	Priority string          `json:"priority,omitempty"`
	ReplyTo  string          `json:"reply_to,omitempty"`
	Tags     []string        `json:"tags,omitempty"`
	Type     string          `json:"type,omitempty"`    // message type: task_result, request, alert, status, chat
	Payload  json.RawMessage `json:"payload,omitempty"` // structured data (type-specific)
}

// AgentMeta is stored in agents/<name>/meta.json.
type AgentMeta struct {
	Name         string `json:"name"`
	Program      string `json:"program,omitempty"`
	Model        string `json:"model,omitempty"`
	Task         string `json:"task,omitempty"`
	Bead         string `json:"bead,omitempty"`
	RegisteredAt string `json:"registered_at"`
}

// Reservation is stored as reservations/{hash}.json.
type Reservation struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Pattern   string `json:"pattern"`
	Repo      string `json:"repo"`
	Exclusive bool   `json:"exclusive"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// Command is stored as commands/{ulid}.json.
type Command struct {
	ID            string `json:"id"`
	TS            string `json:"ts"`
	From          string `json:"from"`
	TargetSession string `json:"target_session"`
	Command       string `json:"command"`
	Args          string `json:"args,omitempty"`
	Status        string `json:"status"`
}

// AgentCard is the machine-readable identity + capability manifest for an agent.
type AgentCard struct {
	Name         string   `json:"name"`
	Skills       []string `json:"skills,omitempty"`       // freeform skill tags
	Status       string   `json:"status,omitempty"`       // "idle", "working", "offline"
	CurrentTask  string   `json:"current_task,omitempty"` // bead ID if working
	LastSeen     string   `json:"last_seen"`              // RFC3339 timestamp (replaces heartbeat)
	RegisteredAt string   `json:"registered_at"`
}

// Agent statuses.
const (
	AgentIdle    = "idle"
	AgentWorking = "working"
	AgentOffline = "offline"
)

// ValidAgentStatuses is the set of recognized agent statuses.
var ValidAgentStatuses = map[string]bool{
	AgentIdle:    true,
	AgentWorking: true,
	AgentOffline: true,
}

// AgentStatus is computed at runtime for relay status.
type AgentStatus struct {
	Name          string        `json:"name"`
	Task          string        `json:"task,omitempty"`
	Skills        []string      `json:"skills,omitempty"`
	CardStatus    string        `json:"card_status,omitempty"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	HeartbeatAge  time.Duration `json:"-"`
	Alive         bool          `json:"alive"`
}

// MaxBodySize is the maximum message body size (64KB per review).
const MaxBodySize = 64 * 1024
