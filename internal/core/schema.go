package core

import (
	"fmt"
	"strings"
	"time"
)

// Validate checks whether the message matches the Relay schema.
//
// Required fields: id, from, to, ts, body.
// Optional fields: subject, thread, tags, priority, reply_to.
func (m Message) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("message.id is required")
	}
	if strings.TrimSpace(m.From) == "" {
		return fmt.Errorf("message.from is required")
	}
	if strings.TrimSpace(m.To) == "" {
		return fmt.Errorf("message.to is required")
	}
	if strings.TrimSpace(m.TS) == "" {
		return fmt.Errorf("message.ts is required")
	}
	if _, err := time.Parse(time.RFC3339, m.TS); err != nil {
		return fmt.Errorf("message.ts must be RFC3339: %w", err)
	}
	if strings.TrimSpace(m.Body) == "" {
		return fmt.Errorf("message.body is required")
	}
	if len(m.Body) > MaxBodySize {
		return fmt.Errorf("message body too large: %d bytes (max %d)", len(m.Body), MaxBodySize)
	}

	for i, tag := range m.Tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("message.tags[%d] must not be empty", i)
		}
	}
	if m.Priority != "" && strings.TrimSpace(m.Priority) == "" {
		return fmt.Errorf("message.priority must not be whitespace")
	}
	if m.Subject != "" && strings.TrimSpace(m.Subject) == "" {
		return fmt.Errorf("message.subject must not be whitespace")
	}
	if m.Thread != "" && strings.TrimSpace(m.Thread) == "" {
		return fmt.Errorf("message.thread must not be whitespace")
	}
	if m.ReplyTo != "" && strings.TrimSpace(m.ReplyTo) == "" {
		return fmt.Errorf("message.reply_to must not be whitespace")
	}
	return nil
}
