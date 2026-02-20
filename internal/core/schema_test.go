package core

import (
	"strings"
	"testing"
	"time"
)

func TestMessageValidateValidMinimal(t *testing.T) {
	msg := Message{
		ID:   NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("expected valid message, got error: %v", err)
	}
}

func TestMessageValidateValidWithOptionalFields(t *testing.T) {
	msg := Message{
		ID:       NewULID(),
		TS:       time.Now().UTC().Format(time.RFC3339),
		From:     "agent-a",
		To:       "agent-b",
		Body:     "hello",
		Subject:  "subject",
		Thread:   "thread-1",
		Tags:     []string{"coordination", "handoff"},
		Priority: "high",
		ReplyTo:  NewULID(),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("expected valid message with optional fields, got error: %v", err)
	}
}

func TestMessageValidateMissingRequiredFields(t *testing.T) {
	base := Message{
		ID:   NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
	}

	tests := []struct {
		name    string
		mutate  func(*Message)
		wantErr string
	}{
		{
			name: "missing id",
			mutate: func(m *Message) {
				m.ID = ""
			},
			wantErr: "message.id is required",
		},
		{
			name: "missing from",
			mutate: func(m *Message) {
				m.From = ""
			},
			wantErr: "message.from is required",
		},
		{
			name: "missing to",
			mutate: func(m *Message) {
				m.To = ""
			},
			wantErr: "message.to is required",
		},
		{
			name: "missing ts",
			mutate: func(m *Message) {
				m.TS = ""
			},
			wantErr: "message.ts is required",
		},
		{
			name: "missing body",
			mutate: func(m *Message) {
				m.Body = ""
			},
			wantErr: "message.body is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := base
			tc.mutate(&msg)
			err := msg.Validate()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestMessageValidateInvalidTimestamp(t *testing.T) {
	msg := Message{
		ID:   NewULID(),
		TS:   "not-a-time",
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
	}
	err := msg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "message.ts must be RFC3339") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMessageValidateRejectsEmptyTag(t *testing.T) {
	msg := Message{
		ID:   NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
		Tags: []string{"ok", ""},
	}
	err := msg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "message.tags[1]") {
		t.Fatalf("unexpected error: %v", err)
	}
}
