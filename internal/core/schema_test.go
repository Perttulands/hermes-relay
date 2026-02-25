package core

import (
	"encoding/json"
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

func TestMessageValidateValidTypes(t *testing.T) {
	for _, typ := range []string{TypeTaskResult, TypeRequest, TypeAlert, TypeStatus, TypeChat} {
		msg := Message{
			ID:   NewULID(),
			TS:   time.Now().UTC().Format(time.RFC3339),
			From: "agent-a",
			To:   "agent-b",
			Body: "hello",
			Type: typ,
		}
		if err := msg.Validate(); err != nil {
			t.Errorf("type %q should be valid, got error: %v", typ, err)
		}
	}
}

func TestMessageValidateInvalidType(t *testing.T) {
	msg := Message{
		ID:   NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
		Type: "bogus",
	}
	err := msg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for invalid type")
	}
	if !strings.Contains(err.Error(), "not a recognized type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMessageValidateEmptyTypeIsValid(t *testing.T) {
	msg := Message{
		ID:   NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "agent-a",
		To:   "agent-b",
		Body: "hello",
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("empty type should be valid (backward compat), got error: %v", err)
	}
}

func TestMessageValidateValidPayload(t *testing.T) {
	msg := Message{
		ID:      NewULID(),
		TS:      time.Now().UTC().Format(time.RFC3339),
		From:    "agent-a",
		To:      "agent-b",
		Body:    "hello",
		Type:    TypeTaskResult,
		Payload: json.RawMessage(`{"status":"success","exit_code":0}`),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("valid payload should pass, got error: %v", err)
	}
}

func TestMessageValidateInvalidPayloadJSON(t *testing.T) {
	msg := Message{
		ID:      NewULID(),
		TS:      time.Now().UTC().Format(time.RFC3339),
		From:    "agent-a",
		To:      "agent-b",
		Body:    "hello",
		Type:    TypeTaskResult,
		Payload: json.RawMessage(`{not valid json}`),
	}
	err := msg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for invalid payload JSON")
	}
	if !strings.Contains(err.Error(), "message.payload must be valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMessageValidatePayloadWithoutType(t *testing.T) {
	// Payload without type is allowed (no coupling)
	msg := Message{
		ID:      NewULID(),
		TS:      time.Now().UTC().Format(time.RFC3339),
		From:    "agent-a",
		To:      "agent-b",
		Body:    "hello",
		Payload: json.RawMessage(`{"data":"value"}`),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("payload without type should be valid, got error: %v", err)
	}
}
