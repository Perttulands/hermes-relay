package core

import "time"

// Message is a single NDJSON line in an agent's inbox.
type Message struct {
	ID       string   `json:"id"`
	TS       string   `json:"ts"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Subject  string   `json:"subject,omitempty"`
	Body     string   `json:"body"`
	Thread   string   `json:"thread,omitempty"`
	Priority string   `json:"priority,omitempty"`
	ReplyTo  string   `json:"reply_to,omitempty"`
	Tags     []string `json:"tags,omitempty"`
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

// AgentStatus is computed at runtime for relay status.
type AgentStatus struct {
	Name          string        `json:"name"`
	Task          string        `json:"task,omitempty"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	HeartbeatAge  time.Duration `json:"-"`
	Alive         bool          `json:"alive"`
}

// MaxBodySize is the maximum message body size (64KB per review).
const MaxBodySize = 64 * 1024
