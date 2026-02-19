package client

import (
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
