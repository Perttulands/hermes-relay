// Package client provides a programmatic Relay API for agents.
package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Perttulands/relay/internal/core"
	"github.com/Perttulands/relay/internal/store"
)

// Message is a Relay message.
type Message = core.Message

// ReadOpts controls message filtering.
type ReadOpts struct {
	From     string
	Thread   string
	Since    time.Time
	Unread   bool
	Last     int
	MarkRead bool
	Type     string
}

// Client wraps relay store operations for agent-centric messaging.
type Client struct {
	store       *store.Dir
	agent       string
	mu          sync.Mutex
	watchOffset int64
}

// NewClient creates a client bound to the current agent.
//
// Directory resolution order:
// 1. explicit dir argument
// 2. RELAY_DIR
// 3. ~/.relay
//
// Agent resolution order:
// 1. RELAY_AGENT
// 2. hostname
func NewClient(dir string) (*Client, error) {
	root, err := resolveDir(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve relay dir: %w", err)
	}

	s, err := store.New(root)
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	agent, err := resolveAgent()
	if err != nil {
		return nil, fmt.Errorf("resolve agent: %w", err)
	}

	return &Client{
		store: s,
		agent: agent,
	}, nil
}

// Send sends a message from the current agent to recipient "to".
func (c *Client) Send(to, body string) error {
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("recipient is required")
	}

	subject := body
	if len(subject) > 80 {
		subject = subject[:80]
	}

	msg := core.Message{
		ID:       core.NewULID(),
		TS:       time.Now().UTC().Format(time.RFC3339),
		From:     c.agent,
		To:       to,
		Subject:  subject,
		Body:     body,
		Priority: "normal",
	}

	return c.store.Send(msg)
}

// SendTyped sends a typed message with an optional structured payload.
// msgType must be one of the recognized types (task_result, request, alert, status, chat).
// payload should be valid JSON or nil.
func (c *Client) SendTyped(to, body, msgType string, payload json.RawMessage) error {
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("recipient is required")
	}

	subject := body
	if len(subject) > 80 {
		subject = subject[:80]
	}

	msg := core.Message{
		ID:       core.NewULID(),
		TS:       time.Now().UTC().Format(time.RFC3339),
		From:     c.agent,
		To:       to,
		Subject:  subject,
		Body:     body,
		Priority: "normal",
	}
	if msgType != "" {
		msg.Type = msgType
	}
	if len(payload) > 0 {
		msg.Payload = payload
	}

	return c.store.Send(msg)
}

// Read returns inbox messages for the current agent.
//
// If Last is unset and Unread is false, it defaults to 20 (same as CLI).
func (c *Client) Read(opts ReadOpts) ([]Message, error) {
	storeOpts := store.ReadOpts{
		From:     opts.From,
		Thread:   opts.Thread,
		Since:    opts.Since,
		Unread:   opts.Unread,
		Last:     opts.Last,
		MarkRead: opts.MarkRead,
		Type:     opts.Type,
	}
	if storeOpts.Last == 0 && !storeOpts.Unread {
		storeOpts.Last = 20
	}
	return c.store.ReadInbox(c.agent, storeOpts)
}

// Watch blocks until new inbox messages are available for the current agent.
//
// Subsequent calls continue from the last observed offset.
func (c *Client) Watch() ([]Message, error) {
	c.mu.Lock()
	offset := c.watchOffset
	c.mu.Unlock()

	msgs, newOffset, err := c.store.WatchInbox(c.agent, offset)
	if err != nil {
		return nil, fmt.Errorf("watch inbox: %w", err)
	}

	c.mu.Lock()
	c.watchOffset = newOffset
	c.mu.Unlock()
	return msgs, nil
}

// AgentCard is a Relay agent card.
type AgentCard = core.AgentCard

// UpdateCard writes or updates the current agent's card.
func (c *Client) UpdateCard(card core.AgentCard) error {
	card.Name = c.agent
	return c.store.WriteCard(card)
}

// GetCard reads the card for the named agent.
func (c *Client) GetCard(agent string) (core.AgentCard, error) {
	return c.store.ReadCard(agent)
}

// ListCards returns cards for all registered agents that have one.
func (c *Client) ListCards() ([]core.AgentCard, error) {
	return c.store.ListCards()
}

func resolveDir(dir string) (string, error) {
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("RELAY_DIR"))
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".relay")
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	return filepath.Clean(dir), nil
}

func resolveAgent() (string, error) {
	if agent := os.Getenv("RELAY_AGENT"); agent != "" {
		return agent, nil
	}
	agent, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("resolve agent name: %w", err)
	}
	if strings.TrimSpace(agent) == "" {
		return "", fmt.Errorf("resolve agent name: empty hostname")
	}
	return agent, nil
}
