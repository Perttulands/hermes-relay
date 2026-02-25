package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Perttulands/relay/internal/core"
	"github.com/Perttulands/relay/internal/store"
)

func setupStore(t *testing.T) (string, *store.Dir) {
	t.Helper()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, s
}

func registerAgents(t *testing.T, s *store.Dir, agents ...string) {
	t.Helper()
	for _, agent := range agents {
		err := s.Register(core.AgentMeta{
			Name:         agent,
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("register %q: %v", agent, err)
		}
	}
}

func TestNewClientExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RELAY_AGENT", "test-agent")

	c, err := NewClient("~/.relay-test")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}

	for _, sub := range []string{"agents", "reservations", "commands", "wake"} {
		path := filepath.Join(home, ".relay-test", sub)
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		} else if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", path)
		}
	}
}

func TestSendAndRead(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "sender", "athena")

	t.Setenv("RELAY_AGENT", "sender")
	sender, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send("athena", "task complete"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_AGENT", "athena")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := receiver.Read(ReadOpts{Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "sender" || msgs[0].To != "athena" {
		t.Fatalf("unexpected routing: from=%q to=%q", msgs[0].From, msgs[0].To)
	}
	if msgs[0].Body != "task complete" {
		t.Fatalf("unexpected body: %q", msgs[0].Body)
	}
}

func TestReadDefaultsToLast20(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "sender", "athena")

	t.Setenv("RELAY_AGENT", "sender")
	sender, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if err := sender.Send("athena", fmt.Sprintf("msg-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("RELAY_AGENT", "athena")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := receiver.Read(ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 20 {
		t.Fatalf("expected 20 messages, got %d", len(msgs))
	}
	if msgs[0].Body != "msg-05" {
		t.Fatalf("expected first message to be msg-05, got %q", msgs[0].Body)
	}
}

func TestWatchReceivesNewMessage(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "watcher", "sender")

	t.Setenv("RELAY_AGENT", "watcher")
	watcher, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan []Message, 1)
	errs := make(chan error, 1)
	go func() {
		msgs, err := watcher.Watch()
		if err != nil {
			errs <- err
			return
		}
		done <- msgs
	}()

	time.Sleep(100 * time.Millisecond)
	t.Setenv("RELAY_AGENT", "sender")
	sender, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send("watcher", "watch-event"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatalf("watch failed: %v", err)
	case msgs := <-done:
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Body != "watch-event" {
			t.Fatalf("unexpected body: %q", msgs[0].Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch timed out")
	}
}

func TestReadFilterByFrom(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "athena", "sender-a", "sender-b")

	t.Setenv("RELAY_AGENT", "sender-a")
	a, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send("athena", "from-a"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_AGENT", "sender-b")
	b, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send("athena", "from-b"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_AGENT", "athena")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := receiver.Read(ReadOpts{From: "sender-a", Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "sender-a" || msgs[0].Body != "from-a" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestSendTyped(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "sender", "target")

	t.Setenv("RELAY_AGENT", "sender")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	payload := json.RawMessage(`{"exit_code":0,"summary":"all tests pass"}`)
	if err := c.SendTyped("target", "task complete", core.TypeTaskResult, payload); err != nil {
		t.Fatal(err)
	}

	// Read as target and verify
	t.Setenv("RELAY_AGENT", "target")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := receiver.Read(ReadOpts{Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != core.TypeTaskResult {
		t.Errorf("expected type=%s, got %q", core.TypeTaskResult, msgs[0].Type)
	}
	if string(msgs[0].Payload) != string(payload) {
		t.Errorf("unexpected payload: %s", msgs[0].Payload)
	}
}

func TestSendTypedWithoutPayload(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "sender", "target")

	t.Setenv("RELAY_AGENT", "sender")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	// Type without payload
	if err := c.SendTyped("target", "status update", core.TypeStatus, nil); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_AGENT", "target")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := receiver.Read(ReadOpts{Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != core.TypeStatus {
		t.Errorf("expected type=%s, got %q", core.TypeStatus, msgs[0].Type)
	}
}

func TestReadFilterByType(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "sender", "reader")

	t.Setenv("RELAY_AGENT", "sender")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	// Send three messages with different types
	c.SendTyped("reader", "alert msg", core.TypeAlert, nil)
	c.SendTyped("reader", "chat msg", core.TypeChat, nil)
	c.Send("reader", "plain msg") // no type

	t.Setenv("RELAY_AGENT", "reader")
	receiver, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	// Filter by alert
	msgs, err := receiver.Read(ReadOpts{Type: core.TypeAlert, Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 alert message, got %d", len(msgs))
	}
	if msgs[0].Type != core.TypeAlert {
		t.Errorf("expected type=alert, got %q", msgs[0].Type)
	}

	// No filter returns all
	msgs, err = receiver.Read(ReadOpts{Last: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestSendTypedEmptyRecipient(t *testing.T) {
	root, _ := setupStore(t)

	t.Setenv("RELAY_AGENT", "sender")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	err = c.SendTyped("", "body", core.TypeChat, nil)
	if err == nil {
		t.Fatal("expected error for empty recipient")
	}
}

// --- Agent card client tests ---

func TestUpdateAndGetCard(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "agent-1")

	t.Setenv("RELAY_AGENT", "agent-1")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	card := core.AgentCard{
		Skills:       []string{"go", "rust"},
		Status:       core.AgentWorking,
		CurrentTask:  "br-42",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := c.UpdateCard(card); err != nil {
		t.Fatal(err)
	}

	got, err := c.GetCard("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent-1" {
		t.Errorf("expected name=agent-1, got %s", got.Name)
	}
	if got.Status != core.AgentWorking {
		t.Errorf("expected status=working, got %s", got.Status)
	}
	if got.CurrentTask != "br-42" {
		t.Errorf("expected current_task=br-42, got %s", got.CurrentTask)
	}
	if len(got.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(got.Skills))
	}
}

func TestListCardsClient(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "agent-1", "agent-2", "agent-3")

	// Write cards for agent-1 and agent-2 only
	t.Setenv("RELAY_AGENT", "agent-1")
	c1, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	c1.UpdateCard(core.AgentCard{
		Skills:       []string{"go"},
		Status:       core.AgentIdle,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	})

	t.Setenv("RELAY_AGENT", "agent-2")
	c2, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}
	c2.UpdateCard(core.AgentCard{
		Skills:       []string{"rust"},
		Status:       core.AgentWorking,
		CurrentTask:  "br-10",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	})

	// ListCards from any client should return both
	cards, err := c2.ListCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
}

func TestGetCardNonexistent(t *testing.T) {
	root, _ := setupStore(t)

	t.Setenv("RELAY_AGENT", "agent-1")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.GetCard("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent card")
	}
}

func TestUpdateCardSetsName(t *testing.T) {
	root, s := setupStore(t)
	registerAgents(t, s, "my-agent")

	t.Setenv("RELAY_AGENT", "my-agent")
	c, err := NewClient(root)
	if err != nil {
		t.Fatal(err)
	}

	// Name is omitted in the card — UpdateCard should set it from the client's agent.
	card := core.AgentCard{
		Status:       core.AgentIdle,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := c.UpdateCard(card); err != nil {
		t.Fatal(err)
	}

	got, err := c.GetCard("my-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "my-agent" {
		t.Errorf("expected name=my-agent, got %s", got.Name)
	}
}
