package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
)

func tempDir(t *testing.T) *Dir {
	t.Helper()
	root := t.TempDir()
	d, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNewCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	_, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"agents", "reservations", "commands", "wake", "chains"} {
		path := filepath.Join(root, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected dir %s to exist: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}
}

func TestRegisterAndReadMeta(t *testing.T) {
	d := tempDir(t)
	meta := core.AgentMeta{
		Name:         "test-agent",
		Program:      "claude-code",
		Model:        "opus",
		Task:         "testing",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.Register(meta); err != nil {
		t.Fatal(err)
	}

	got, err := d.ReadMeta("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test-agent" || got.Program != "claude-code" {
		t.Errorf("unexpected meta: %+v", got)
	}

	// Heartbeat should exist
	hb, err := d.ReadHeartbeat("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(hb) > 5*time.Second {
		t.Errorf("heartbeat too old: %v", hb)
	}
}

func TestRegisterUpdateExisting(t *testing.T) {
	d := tempDir(t)
	meta := core.AgentMeta{
		Name:         "agent",
		Task:         "task1",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.Register(meta); err != nil {
		t.Fatal(err)
	}
	meta.Task = "task2"
	if err := d.Register(meta); err != nil {
		t.Fatal(err)
	}
	got, _ := d.ReadMeta("agent")
	if got.Task != "task2" {
		t.Errorf("expected task2, got %s", got.Task)
	}
}

func TestListAgents(t *testing.T) {
	d := tempDir(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		d.Register(core.AgentMeta{Name: name, RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	agents, err := d.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}
}

func TestSendAndRead(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	d.Register(core.AgentMeta{Name: "bob", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	msg := core.Message{
		ID:       core.NewULID(),
		TS:       time.Now().UTC().Format(time.RFC3339),
		From:     "bob",
		To:       "alice",
		Subject:  "hello",
		Body:     "world",
		Priority: "normal",
	}
	if err := d.Send(msg); err != nil {
		t.Fatal(err)
	}

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Body != "world" {
		t.Errorf("expected body=world, got %s", msgs[0].Body)
	}
}

func TestSendNormalizesLegacyMessageShape(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Legacy ad-hoc writers may omit id/ts/subject/priority.
	msg := core.Message{
		From: "bob",
		To:   "alice",
		Body: "legacy body",
	}
	if err := d.Send(msg); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	got := msgs[0]
	if got.ID == "" {
		t.Fatal("expected generated id")
	}
	if got.TS == "" {
		t.Fatal("expected generated ts")
	}
	if got.Subject != "legacy body" {
		t.Fatalf("expected subject to default to body, got %q", got.Subject)
	}
	if got.Priority != "normal" {
		t.Fatalf("expected priority normal, got %q", got.Priority)
	}
}

func TestSendToNonexistentRecipient(t *testing.T) {
	d := tempDir(t)
	msg := core.Message{
		ID:   core.NewULID(),
		From: "bob",
		To:   "nobody",
		Body: "test",
	}
	err := d.Send(msg)
	if err == nil {
		t.Fatal("expected error for nonexistent recipient")
	}
}

func TestSendBodySizeLimit(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	msg := core.Message{
		ID:   core.NewULID(),
		From: "bob",
		To:   "alice",
		Body: string(make([]byte, core.MaxBodySize+1)),
	}
	err := d.Send(msg)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
}

func TestReadFilters(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	now := time.Now().UTC()
	for i, from := range []string{"bob", "charlie", "bob"} {
		msg := core.Message{
			ID:     core.NewULID(),
			TS:     now.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			From:   from,
			To:     "alice",
			Body:   fmt.Sprintf("msg %d", i),
			Thread: "t1",
		}
		d.Send(msg)
	}

	// Filter by from
	msgs, _ := d.ReadInbox("alice", ReadOpts{From: "bob"})
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages from bob, got %d", len(msgs))
	}

	// Filter by thread
	msgs, _ = d.ReadInbox("alice", ReadOpts{Thread: "t1"})
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages in thread t1, got %d", len(msgs))
	}

	// Last N
	msgs, _ = d.ReadInbox("alice", ReadOpts{Last: 1})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message with last=1, got %d", len(msgs))
	}
}

func TestUnreadCursor(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send 2 messages
	for i := 0; i < 2; i++ {
		d.Send(core.Message{
			ID:   core.NewULID(),
			TS:   time.Now().UTC().Format(time.RFC3339),
			From: "bob", To: "alice", Body: fmt.Sprintf("msg%d", i),
		})
	}

	// Read with mark-read
	msgs, _ := d.ReadInbox("alice", ReadOpts{MarkRead: true})
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}

	// Now unread should be empty
	msgs, _ = d.ReadInbox("alice", ReadOpts{Unread: true})
	if len(msgs) != 0 {
		t.Errorf("expected 0 unread, got %d", len(msgs))
	}

	// Send another
	d.Send(core.Message{
		ID:   core.NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "msg2",
	})

	// Should see 1 unread
	msgs, _ = d.ReadInbox("alice", ReadOpts{Unread: true})
	if len(msgs) != 1 {
		t.Errorf("expected 1 unread, got %d", len(msgs))
	}
}

func TestWatchInbox(t *testing.T) {
	d := tempDir(t)
	if err := d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := d.Register(core.AgentMeta{Name: "bob", RegisteredAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		msgs []core.Message
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		msgs, _, err := d.WatchInbox("alice", 0)
		ch <- result{msgs: msgs, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	if err := d.Send(core.Message{
		ID:      core.NewULID(),
		TS:      time.Now().UTC().Format(time.RFC3339),
		From:    "bob",
		To:      "alice",
		Subject: "watch-test",
		Body:    "watch-test",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("watch error: %v", got.err)
		}
		if len(got.msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(got.msgs))
		}
		if got.msgs[0].Subject != "watch-test" {
			t.Fatalf("unexpected message subject: %q", got.msgs[0].Subject)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch timed out waiting for message")
	}
}

func TestPartialLineToleranceDuringRead(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write a valid message then append garbage (simulating crash)
	msg := core.Message{
		ID:   core.NewULID(),
		TS:   time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "good",
	}
	d.Send(msg)

	inbox := filepath.Join(d.AgentDir("alice"), "inbox.jsonl")
	f, _ := os.OpenFile(inbox, os.O_WRONLY|os.O_APPEND, 0644)
	f.Write([]byte(`{"id":"broken","ts":"bad`)) // partial JSON
	f.Close()

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 valid message (partial line skipped), got %d", len(msgs))
	}
	if msgs[0].Body != "good" {
		t.Errorf("expected body=good, got %s", msgs[0].Body)
	}
}

func TestConcurrentSend(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "inbox-owner", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	const numWriters = 20
	const msgsPerWriter = 50
	var wg sync.WaitGroup

	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < msgsPerWriter; i++ {
				msg := core.Message{
					ID:       core.NewULID(),
					TS:       time.Now().UTC().Format(time.RFC3339),
					From:     fmt.Sprintf("writer-%d", writer),
					To:       "inbox-owner",
					Body:     fmt.Sprintf("message %d from writer %d", i, writer),
					Priority: "normal",
				}
				if err := d.Send(msg); err != nil {
					t.Errorf("send failed: %v", err)
				}
			}
		}(w)
	}
	wg.Wait()

	msgs, err := d.ReadInbox("inbox-owner", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	expected := numWriters * msgsPerWriter
	if len(msgs) != expected {
		t.Errorf("expected %d messages, got %d (lost %d)", expected, len(msgs), expected-len(msgs))
	}

	// Verify each message is valid JSON by checking IDs are unique
	seen := make(map[string]bool)
	for _, m := range msgs {
		if seen[m.ID] {
			t.Errorf("duplicate message ID: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestReservation(t *testing.T) {
	d := tempDir(t)

	res := core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "src/auth/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if err := d.Reserve(res); err != nil {
		t.Fatal(err)
	}

	// Should fail on duplicate
	err := d.Reserve(res)
	if err == nil {
		t.Fatal("expected conflict error")
	}

	// List
	list, err := d.ListReservations()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 reservation, got %d", len(list))
	}

	// Release
	if err := d.Release("agent-1", "/repo", "src/auth/**"); err != nil {
		t.Fatal(err)
	}
	list, _ = d.ListReservations()
	if len(list) != 0 {
		t.Errorf("expected 0 after release, got %d", len(list))
	}
}

func TestReservationOwnership(t *testing.T) {
	d := tempDir(t)
	res := core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "file.go",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	d.Reserve(res)

	err := d.Release("agent-2", "/repo", "file.go")
	if err == nil {
		t.Fatal("expected ownership error")
	}
}

func TestReleaseAll(t *testing.T) {
	d := tempDir(t)
	for i, pattern := range []string{"a.go", "b.go", "c.go"} {
		d.Reserve(core.Reservation{
			ID:        core.NewULID(),
			Agent:     "agent-1",
			Pattern:   pattern,
			Repo:      "/repo",
			Exclusive: true,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
		_ = i
	}
	// One by different agent
	d.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-2",
		Pattern:   "d.go",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})

	count, err := d.ReleaseAll("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 released, got %d", count)
	}
	list, _ := d.ListReservations()
	if len(list) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(list))
	}
}

func TestPatternsOverlap(t *testing.T) {
	cases := []struct {
		a, b    string
		overlap bool
	}{
		// Original cases
		{"src/auth/**", "src/auth/login.go", true},
		{"src/**", "src/auth/login.go", true},
		{"src/a/*", "src/b/*", false},
		{"*.go", "src/main.go", false},       // different prefix, no ** in either
		{"**", "anything", true},             // ** overlaps everything
		{"src/auth/**", "src/auth/**", true}, // exact match
		{"src/auth/**", "src/api/**", false}, // different subtrees
		{"*.go", "*.go", true},               // same pattern

		// ** as second arg (line 578)
		{"anything", "**", true},

		// bNorm prefix of aNorm (line 587)
		{"src/auth/login.go", "src/auth/**", true},

		// Original pattern starts with normalized+/ (line 590)
		{"lib/**", "lib/util.go", true},
		{"lib/util.go", "lib/**", true},

		// Same directory, wildcard extension overlap (lines 597-609)
		{"src/*.go", "src/*.go", true},
		{"src/*.go", "src/main.go", true},
		{"src/*.go", "src/*.rs", false},
		{"src/*", "src/main.go", false},

		// ** inside pattern (not suffix), concrete other (lines 613-625)
		{"src/**/test.go", "src/pkg/test.go", true},
		{"lib/**/test.go", "src/pkg/test.go", false},
		{"src/pkg/test.go", "src/**/test.go", true},
		{"src/pkg/test.go", "lib/**/test.go", false},

		// Concrete file vs wildcard in same dir (b is wildcard, reversed)
		{"src/main.go", "src/*.go", true},

		// Wildcard in different dirs (no ** means no cross-dir match)
		{"*.go", "src/*.go", false},

		// Nested ** with extension wildcard
		{"src/**/*.go", "src/pkg/test.go", true},
		{"src/pkg/test.go", "src/**/*.go", true},

		// ** with extension in root
		{"**/*.go", "src/main.go", true},

		// Same-dir wildcard: one has no extension, other has extension
		{"src/*", "src/*.go", false},

		// Disjoint concrete files
		{"src/a.go", "src/b.go", false},
		{"lib/x.go", "src/y.go", false},
	}
	for _, tc := range cases {
		got := patternsOverlap(tc.a, tc.b)
		if got != tc.overlap {
			t.Errorf("patternsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.overlap)
		}
	}
}

func TestCheckOverlap(t *testing.T) {
	d := tempDir(t)
	d.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "src/auth/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})

	conflicts, err := d.CheckOverlap("agent-2", "/repo", "src/auth/login.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}

	// Same agent shouldn't conflict with self
	conflicts, _ = d.CheckOverlap("agent-1", "/repo", "src/auth/login.go")
	if len(conflicts) != 0 {
		t.Errorf("expected 0 self-conflicts, got %d", len(conflicts))
	}
}

func TestCommand(t *testing.T) {
	d := tempDir(t)

	cmd := core.Command{
		ID:            core.NewULID(),
		TS:            time.Now().UTC().Format(time.RFC3339),
		From:          "agent-1",
		TargetSession: "agent:main:main",
		Command:       "/verify",
		Args:          "repo br-42",
		Status:        "pending",
	}
	if err := d.CreateCommand(cmd); err != nil {
		t.Fatal(err)
	}

	cmds, err := d.ListCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Status != "pending" {
		t.Errorf("expected pending, got %s", cmds[0].Status)
	}

	// Consume
	ok, err := d.ConsumeCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected to win claim")
	}

	// Second consume should fail
	ok, err = d.ConsumeCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected to lose claim")
	}

	// Status should now be consumed
	cmds, _ = d.ListCommands()
	if cmds[0].Status != "consumed" {
		t.Errorf("expected consumed, got %s", cmds[0].Status)
	}
}

func TestGCExpiredReservations(t *testing.T) {
	d := tempDir(t)
	// Create an expired reservation
	d.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "expired.go",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})
	// Create a valid reservation
	d.Reserve(core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-1",
		Pattern:   "valid.go",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})

	result, err := d.GC(30*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredReservations != 1 {
		t.Errorf("expected 1 expired cleaned, got %d", result.ExpiredReservations)
	}
	list, _ := d.ListReservations()
	if len(list) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(list))
	}
}

func TestUpdateTask(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", Task: "initial", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	if err := d.UpdateTask("agent", "updated"); err != nil {
		t.Fatal(err)
	}
	meta, _ := d.ReadMeta("agent")
	if meta.Task != "updated" {
		t.Errorf("expected task=updated, got %s", meta.Task)
	}
}

func TestTouchWake(t *testing.T) {
	d := tempDir(t)
	if err := d.TouchWake("test message"); err != nil {
		t.Fatal(err)
	}

	trigger := filepath.Join(d.Root, "wake", "trigger")
	if _, err := os.Stat(trigger); err != nil {
		t.Errorf("trigger file not created: %v", err)
	}
	msg := filepath.Join(d.Root, "wake", "last-message")
	data, err := os.ReadFile(msg)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "test message\n" {
		t.Errorf("unexpected message: %q", data)
	}
}

func TestULIDOrdering(t *testing.T) {
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = core.NewULID()
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("ULID %d (%s) not greater than %d (%s)", i, ids[i], i-1, ids[i-1])
		}
	}
}

func TestReadInboxFilterByType(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send messages with different types
	for _, typ := range []string{core.TypeTaskResult, core.TypeAlert, core.TypeChat, ""} {
		msg := core.Message{
			ID:   core.NewULID(),
			TS:   time.Now().UTC().Format(time.RFC3339),
			From: "bob",
			To:   "alice",
			Body: fmt.Sprintf("msg type=%s", typ),
			Type: typ,
		}
		if err := d.Send(msg); err != nil {
			t.Fatal(err)
		}
	}

	// Filter by task_result
	msgs, err := d.ReadInbox("alice", ReadOpts{Type: core.TypeTaskResult})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 task_result message, got %d", len(msgs))
	}

	// Filter by alert
	msgs, err = d.ReadInbox("alice", ReadOpts{Type: core.TypeAlert})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 alert message, got %d", len(msgs))
	}

	// No filter returns all
	msgs, err = d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Errorf("expected 4 total messages, got %d", len(msgs))
	}

	// Filter by type that has no matches
	msgs, err = d.ReadInbox("alice", ReadOpts{Type: core.TypeRequest})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 request messages, got %d", len(msgs))
	}
}

func TestReservationHash(t *testing.T) {
	h1 := ReservationHash("/repo", "src/**")
	h2 := ReservationHash("/repo", "src/**")
	h3 := ReservationHash("/repo", "lib/**")

	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hash")
	}
}

// --- Metrics tests ---

func TestMetricsEmptyStore(t *testing.T) {
	d := tempDir(t)
	m, err := d.Metrics(5 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if m.Agents != 0 || m.AliveAgents != 0 || m.StaleAgents != 0 {
		t.Errorf("empty store agents: got %+v", m)
	}
	if m.TotalMessages != 0 {
		t.Errorf("empty store messages: got %d", m.TotalMessages)
	}
	if m.Reservations != 0 || m.ActiveReservations != 0 || m.ExpiredReservations != 0 {
		t.Errorf("empty store reservations: got %+v", m)
	}
	if m.Commands != 0 || m.PendingCommands != 0 {
		t.Errorf("empty store commands: got %+v", m)
	}
}

func TestMetricsWithData(t *testing.T) {
	d := tempDir(t)

	// Register 3 agents
	for _, name := range []string{"alive-1", "alive-2", "stale-1"} {
		d.Register(core.AgentMeta{Name: name, RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	}

	// Backdate stale-1's heartbeat so it appears stale (>5min)
	hbPath := filepath.Join(d.AgentDir("stale-1"), "heartbeat")
	old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	os.WriteFile(hbPath, []byte(old+"\n"), 0644)

	// Send 3 messages to alive-1
	for i := 0; i < 3; i++ {
		d.Send(core.Message{
			ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
			From: "alive-2", To: "alive-1", Body: fmt.Sprintf("msg%d", i),
		})
	}

	// Create 1 active and 1 expired reservation
	d.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "alive-1", Pattern: "a.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	d.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "alive-2", Pattern: "b.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	// Create 2 commands: 1 pending, 1 consumed
	cmd1 := core.Command{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "alive-1", TargetSession: "t", Command: "/test", Status: "pending",
	}
	d.CreateCommand(cmd1)
	cmd2 := core.Command{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "alive-2", TargetSession: "t", Command: "/verify", Status: "pending",
	}
	d.CreateCommand(cmd2)
	d.ConsumeCommand(cmd2.ID)

	m, err := d.Metrics(5 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if m.Agents != 3 {
		t.Errorf("agents: want 3, got %d", m.Agents)
	}
	if m.AliveAgents != 2 {
		t.Errorf("alive: want 2, got %d", m.AliveAgents)
	}
	if m.StaleAgents != 1 {
		t.Errorf("stale: want 1, got %d", m.StaleAgents)
	}
	if m.TotalMessages != 3 {
		t.Errorf("messages: want 3, got %d", m.TotalMessages)
	}
	if m.Reservations != 2 {
		t.Errorf("reservations: want 2, got %d", m.Reservations)
	}
	if m.ActiveReservations != 1 {
		t.Errorf("active reservations: want 1, got %d", m.ActiveReservations)
	}
	if m.ExpiredReservations != 1 {
		t.Errorf("expired reservations: want 1, got %d", m.ExpiredReservations)
	}
	if m.Commands != 2 {
		t.Errorf("commands: want 2, got %d", m.Commands)
	}
	if m.PendingCommands != 1 {
		t.Errorf("pending commands: want 1, got %d", m.PendingCommands)
	}
}

// BenchmarkSend benchmarks the flock-guarded append.
func BenchmarkSend(b *testing.B) {
	root := b.TempDir()
	d, _ := New(root)
	d.Register(core.AgentMeta{Name: "target", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Send(core.Message{
			ID:       core.NewULID(),
			TS:       time.Now().UTC().Format(time.RFC3339),
			From:     "sender",
			To:       "target",
			Body:     "benchmark message",
			Priority: "normal",
		})
	}
}

// Stress test: 20 goroutines × 1000 messages per review US-032.
func TestStressConcurrentSend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "target", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	const numWriters = 20
	const msgsPerWriter = 1000
	var wg sync.WaitGroup

	start := time.Now()
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < msgsPerWriter; i++ {
				msg := core.Message{
					ID:       core.NewULID(),
					TS:       time.Now().UTC().Format(time.RFC3339),
					From:     fmt.Sprintf("writer-%d", writer),
					To:       "target",
					Body:     fmt.Sprintf("stress message %d from writer %d", i, writer),
					Priority: "normal",
				}
				if err := d.Send(msg); err != nil {
					t.Errorf("send failed: %v", err)
				}
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	msgs, err := d.ReadInbox("target", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	expected := numWriters * msgsPerWriter
	if len(msgs) != expected {
		t.Errorf("ZERO LOSS VIOLATED: expected %d messages, got %d (lost %d)", expected, len(msgs), expected-len(msgs))
	}

	// Verify uniqueness
	seen := make(map[string]bool)
	for _, m := range msgs {
		if seen[m.ID] {
			t.Errorf("duplicate message ID: %s", m.ID)
		}
		seen[m.ID] = true
	}

	// Verify valid JSON
	inboxPath := filepath.Join(d.AgentDir("target"), "inbox.jsonl")
	data, _ := os.ReadFile(inboxPath)
	var lineCount int
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		lineCount++
		if !json.Valid([]byte(line)) {
			t.Errorf("invalid JSON line: %s", line[:min(len(line), 100)])
		}
	}
	if lineCount != expected {
		t.Errorf("expected %d lines, got %d", expected, lineCount)
	}

	t.Logf("Stress test: %d messages in %v (%.0f msgs/sec)", expected, elapsed, float64(expected)/elapsed.Seconds())
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// --- Agent card tests ---

func TestWriteAndReadCard(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	card := core.AgentCard{
		Name:         "agent",
		Skills:       []string{"go", "rust"},
		Status:       core.AgentIdle,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.WriteCard(card); err != nil {
		t.Fatal(err)
	}

	got, err := d.ReadCard("agent")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent" {
		t.Errorf("expected name=agent, got %s", got.Name)
	}
	if got.Status != core.AgentIdle {
		t.Errorf("expected status=idle, got %s", got.Status)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "go" || got.Skills[1] != "rust" {
		t.Errorf("unexpected skills: %v", got.Skills)
	}
	if got.LastSeen == "" {
		t.Error("expected LastSeen to be set")
	}
}

func TestWriteCardInvalidStatus(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	card := core.AgentCard{
		Name:   "agent",
		Status: "invalid-status",
	}
	err := d.WriteCard(card)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestWriteCardRequiresName(t *testing.T) {
	d := tempDir(t)
	card := core.AgentCard{Status: core.AgentIdle}
	err := d.WriteCard(card)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestHeartbeatUpdatesCardLastSeen(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write initial card with a backdated LastSeen
	oldTime := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	card := core.AgentCard{
		Name:         "agent",
		Status:       core.AgentIdle,
		LastSeen:     oldTime,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.writeCardRaw(card); err != nil {
		t.Fatal(err)
	}

	// Heartbeat should update card's LastSeen to ~now
	if err := d.Heartbeat("agent"); err != nil {
		t.Fatal(err)
	}

	afterHB, err := d.ReadCard("agent")
	if err != nil {
		t.Fatal(err)
	}
	if afterHB.LastSeen == oldTime {
		t.Error("expected LastSeen to be updated after heartbeat")
	}
	// Verify the new LastSeen is recent
	ts, parseErr := time.Parse(time.RFC3339, afterHB.LastSeen)
	if parseErr != nil {
		t.Fatalf("invalid LastSeen timestamp: %v", parseErr)
	}
	if time.Since(ts) > 5*time.Second {
		t.Errorf("LastSeen too old after heartbeat: %v", ts)
	}
}

func TestHeartbeatWithoutCardStillWorks(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// No card.json exists — heartbeat should still succeed
	if err := d.Heartbeat("agent"); err != nil {
		t.Fatal(err)
	}

	hb, err := d.ReadHeartbeat("agent")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(hb) > 5*time.Second {
		t.Errorf("heartbeat too old: %v", hb)
	}
}

func TestListCards(t *testing.T) {
	d := tempDir(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		d.Register(core.AgentMeta{Name: name, RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	// Only alpha and gamma have cards
	d.WriteCard(core.AgentCard{Name: "alpha", Status: core.AgentIdle, RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	d.WriteCard(core.AgentCard{Name: "gamma", Status: core.AgentWorking, CurrentTask: "br-42", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	cards, err := d.ListCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
}

func TestCardForwardCompat(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write card with future field that the struct doesn't know about
	rawJSON := `{"name":"agent","registered_at":"2026-02-25T00:00:00Z","last_seen":"2026-02-25T00:00:00Z","future_field":"hello"}`
	cardPath := filepath.Join(d.AgentDir("agent"), "card.json")
	if err := os.WriteFile(cardPath, []byte(rawJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Should still read successfully, ignoring unknown fields
	card, err := d.ReadCard("agent")
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "agent" {
		t.Errorf("expected name=agent, got %s", card.Name)
	}
}

func TestCardMissingFields(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Minimal card — only required fields
	rawJSON := `{"name":"agent","registered_at":"2026-02-25T00:00:00Z","last_seen":"2026-02-25T00:00:00Z"}`
	cardPath := filepath.Join(d.AgentDir("agent"), "card.json")
	if err := os.WriteFile(cardPath, []byte(rawJSON), 0644); err != nil {
		t.Fatal(err)
	}

	card, err := d.ReadCard("agent")
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "agent" {
		t.Errorf("expected name=agent, got %s", card.Name)
	}
	if card.Status != "" {
		t.Errorf("expected empty status, got %s", card.Status)
	}
	if card.Skills != nil {
		t.Errorf("expected nil skills, got %v", card.Skills)
	}
}

func TestReadHeartbeatTimePrefersCard(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write card with a known LastSeen
	futureTime := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	card := core.AgentCard{
		Name:         "agent",
		LastSeen:     futureTime,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.writeCardRaw(card); err != nil {
		t.Fatal(err)
	}

	hbt, err := d.ReadHeartbeatTime("agent")
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := time.Parse(time.RFC3339, futureTime)
	if !hbt.Equal(expected) {
		t.Errorf("expected ReadHeartbeatTime to return card's LastSeen %v, got %v", expected, hbt)
	}
}

func TestReadHeartbeatTimeFallsBackToFile(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// No card.json — should fall back to heartbeat file
	hbt, err := d.ReadHeartbeatTime("agent")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(hbt) > 5*time.Second {
		t.Errorf("expected recent heartbeat time, got %v ago", time.Since(hbt))
	}
}

// --- Targeted coverage tests ---

func TestReadHeartbeatNonexistent(t *testing.T) {
	d := tempDir(t)
	_, err := d.ReadHeartbeat("no-such-agent")
	if err == nil {
		t.Error("expected error for nonexistent heartbeat")
	}
}

func TestReadHeartbeatMalformedTimestamp(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	// Write garbage to heartbeat file
	hbPath := filepath.Join(d.AgentDir("agent"), "heartbeat")
	os.WriteFile(hbPath, []byte("not-a-timestamp\n"), 0644)

	_, err := d.ReadHeartbeat("agent")
	if err == nil {
		t.Error("expected error for malformed heartbeat")
	}
}

func TestReadMetaNonexistent(t *testing.T) {
	d := tempDir(t)
	_, err := d.ReadMeta("no-such-agent")
	if err == nil {
		t.Error("expected error for nonexistent meta")
	}
}

func TestReadMetaMalformedJSON(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	metaPath := filepath.Join(d.AgentDir("agent"), "meta.json")
	os.WriteFile(metaPath, []byte("{not valid json"), 0644)

	_, err := d.ReadMeta("agent")
	if err == nil {
		t.Error("expected error for malformed meta")
	}
}

func TestReadCardMalformedJSON(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	cardPath := filepath.Join(d.AgentDir("agent"), "card.json")
	os.WriteFile(cardPath, []byte("{broken"), 0644)

	_, err := d.ReadCard("agent")
	if err == nil {
		t.Error("expected error for malformed card JSON")
	}
}

func TestListAgentsEmptyDir(t *testing.T) {
	d := tempDir(t)
	agents, err := d.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents in empty store, got %d", len(agents))
	}
}

func TestListAgentsSkipsFiles(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "real-agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	// Create a file (not dir) in agents/
	os.WriteFile(filepath.Join(d.Root, "agents", "not-a-dir"), []byte("x"), 0644)

	agents, err := d.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent (file skipped), got %d", len(agents))
	}
}

func TestSendToMissingRecipientDir(t *testing.T) {
	d := tempDir(t)
	msg := core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "nonexistent", Body: "test", Priority: "normal",
	}
	err := d.Send(msg)
	if err == nil {
		t.Error("expected error for missing recipient")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateTaskNonexistentAgent(t *testing.T) {
	d := tempDir(t)
	err := d.UpdateTask("no-agent", "task")
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestConsumeCommandNonexistent(t *testing.T) {
	d := tempDir(t)
	// Consume a command that was never created — should still work (create sidecar)
	ok, err := d.ConsumeCommand("nonexistent-id")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected first consume to succeed")
	}
	// Second consume should fail
	ok, err = d.ConsumeCommand("nonexistent-id")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected second consume to return false")
	}
}

func TestReserveDuplicateNoExistingReadable(t *testing.T) {
	d := tempDir(t)
	res := core.Reservation{
		ID: core.NewULID(), Agent: "agent", Pattern: "file.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if err := d.Reserve(res); err != nil {
		t.Fatal(err)
	}

	// Corrupt the reservation file so readReservation fails
	hash := ReservationHash("/r", "file.go")
	path := filepath.Join(d.Root, "reservations", hash+".json")
	os.WriteFile(path, []byte("{broken json"), 0644)

	// Try to reserve again — should get conflict with fallback error message
	err := d.Reserve(res)
	if err == nil {
		t.Error("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestReleaseNonexistentReservation(t *testing.T) {
	d := tempDir(t)
	err := d.Release("agent", "/repo", "no-such-pattern")
	if err == nil {
		t.Error("expected error for nonexistent reservation")
	}
}

func TestListReservationsSkipsNonJSON(t *testing.T) {
	d := tempDir(t)
	// Create a non-JSON file in reservations/
	os.WriteFile(filepath.Join(d.Root, "reservations", "readme.txt"), []byte("ignore"), 0644)
	// Create a broken JSON file
	os.WriteFile(filepath.Join(d.Root, "reservations", "broken.json"), []byte("{bad"), 0644)

	list, err := d.ListReservations()
	if err != nil {
		t.Fatal(err)
	}
	// Should skip both: non-json and broken json
	if len(list) != 0 {
		t.Errorf("expected 0 valid reservations, got %d", len(list))
	}
}

func TestListCommandsSkipsNonJSON(t *testing.T) {
	d := tempDir(t)
	// Create non-json files
	os.WriteFile(filepath.Join(d.Root, "commands", "readme.txt"), []byte("ignore"), 0644)
	os.WriteFile(filepath.Join(d.Root, "commands", "broken.json"), []byte("{bad"), 0644)

	cmds, err := d.ListCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected 0 valid commands, got %d", len(cmds))
	}
}

func TestIsExpiredMalformedTimestamp(t *testing.T) {
	r := core.Reservation{ExpiresAt: "not-a-date"}
	// isExpired should return true for unparseable timestamps so GC collects them
	if !isExpired(r) {
		t.Error("expected true for malformed expiry — unparseable timestamps must be treated as expired")
	}
}

func TestIsExpiredFutureReservation(t *testing.T) {
	r := core.Reservation{
		ExpiresAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	}
	if isExpired(r) {
		t.Error("expected false for future expiry")
	}
}

func TestIsExpiredPastReservation(t *testing.T) {
	r := core.Reservation{
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	}
	if !isExpired(r) {
		t.Error("expected true for past expiry")
	}
}

func TestIsExpiredEmptyTimestamp(t *testing.T) {
	r := core.Reservation{ExpiresAt: ""}
	if !isExpired(r) {
		t.Error("expected true for empty expiry — must be treated as expired")
	}
}

func TestGCRemovesUnparsableReservation(t *testing.T) {
	d := tempDir(t)

	// Create a reservation with an unparseable ExpiresAt directly via Reserve
	bad := core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-bad",
		Pattern:   "src/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: "not-a-valid-timestamp",
	}
	if err := d.Reserve(bad); err != nil {
		t.Fatal(err)
	}

	// Also create a valid active reservation that should survive GC
	good := core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-good",
		Pattern:   "docs/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	}
	if err := d.Reserve(good); err != nil {
		t.Fatal(err)
	}

	// Run GC
	result, err := d.GC(30*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredReservations != 1 {
		t.Errorf("expected 1 expired reservation removed, got %d", result.ExpiredReservations)
	}

	// Verify the bad reservation is gone and the good one survives
	remaining, err := d.ListReservations()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining reservation, got %d", len(remaining))
	}
	if remaining[0].Agent != "agent-good" {
		t.Errorf("expected surviving reservation from agent-good, got %s", remaining[0].Agent)
	}
}

func TestCheckOverlapIgnoresUnparsableReservation(t *testing.T) {
	d := tempDir(t)

	// Create a reservation with unparseable ExpiresAt
	bad := core.Reservation{
		ID:        core.NewULID(),
		Agent:     "agent-bad",
		Pattern:   "src/**",
		Repo:      "/repo",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: "garbage-date",
	}
	if err := d.Reserve(bad); err != nil {
		t.Fatal(err)
	}

	// Another agent checking overlap on same pattern should see no conflicts
	// because the unparseable reservation is treated as expired
	conflicts, err := d.CheckOverlap("agent-new", "/repo", "src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts (unparseable reservation treated as expired), got %d", len(conflicts))
	}
}

func TestWatchInboxUnregisteredAgent(t *testing.T) {
	d := tempDir(t)
	_, _, err := d.WatchInbox("nonexistent", 0)
	if err == nil {
		t.Error("expected error for unregistered agent")
	}
}

func TestReadHeartbeatTimeCardWithBadLastSeen(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write card with bad LastSeen — should fall back to heartbeat file
	card := core.AgentCard{
		Name:         "agent",
		LastSeen:     "not-a-date",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	d.writeCardRaw(card)

	hbt, err := d.ReadHeartbeatTime("agent")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(hbt) > 5*time.Second {
		t.Errorf("expected recent time, got %v ago", time.Since(hbt))
	}
}

func TestReadHeartbeatTimeCardEmptyLastSeen(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write card with empty LastSeen
	card := core.AgentCard{
		Name:         "agent",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	d.writeCardRaw(card)

	hbt, err := d.ReadHeartbeatTime("agent")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(hbt) > 5*time.Second {
		t.Errorf("expected recent time from heartbeat file, got %v ago", time.Since(hbt))
	}
}

func TestReadInboxEmptyFile(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Create empty inbox
	inbox := filepath.Join(d.AgentDir("agent"), "inbox.jsonl")
	os.WriteFile(inbox, []byte(""), 0644)

	msgs, err := d.ReadInbox("agent", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestReadInboxNonexistentFile(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// No inbox file — should return empty slice, not error
	msgs, err := d.ReadInbox("agent", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0, got %d", len(msgs))
	}
}

func TestGCWithMixedCommands(t *testing.T) {
	d := tempDir(t)

	// Create a pending command (not consumed) — should not be cleaned
	cmd1 := core.Command{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "agent", TargetSession: "t", Command: "/test", Status: "pending",
	}
	d.CreateCommand(cmd1)

	// Create a recently consumed command — should not be cleaned
	cmd2 := core.Command{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "agent", TargetSession: "t", Command: "/test2", Status: "pending",
	}
	d.CreateCommand(cmd2)
	d.ConsumeCommand(cmd2.ID)

	result, err := d.GC(30*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	// Recent consumed should not be cleaned
	if result.OldCommands != 0 {
		t.Errorf("expected 0 old commands cleaned, got %d", result.OldCommands)
	}
}

func TestListAgentsMissingDir(t *testing.T) {
	// Create a Dir with a root that exists but where agents/ has been removed
	root := t.TempDir()
	d, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Remove the agents directory
	os.RemoveAll(filepath.Join(root, "agents"))

	agents, err := d.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestReadMessagesSinceOffsetBeyondFile(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write a message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "msg",
	})

	inbox := filepath.Join(d.AgentDir("agent"), "inbox.jsonl")
	// Read with offset way past file end
	msgs, newOffset, err := readMessagesSince(inbox, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
	// Offset should be clamped to file size
	info, _ := os.Stat(inbox)
	if newOffset < info.Size() {
		t.Errorf("expected offset >= file size, got %d", newOffset)
	}
}

func TestReadMessagesSincePartialLine(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Write a complete message plus a partial trailing line
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "complete",
	})
	inbox := filepath.Join(d.AgentDir("agent"), "inbox.jsonl")
	f, _ := os.OpenFile(inbox, os.O_WRONLY|os.O_APPEND, 0644)
	f.Write([]byte(`{"id":"partial","body":"incomplete`))
	f.Close()

	msgs, _, err := readMessagesSince(inbox, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 complete message, got %d", len(msgs))
	}
}

func TestReadMessagesSinceMalformedJSON(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	inbox := filepath.Join(d.AgentDir("agent"), "inbox.jsonl")
	// Write a broken JSON line followed by a valid one
	f, _ := os.OpenFile(inbox, os.O_WRONLY|os.O_CREATE, 0644)
	f.Write([]byte("{broken json}\n"))
	good := core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "s", To: "agent", Body: "ok",
	}
	line, _ := json.Marshal(good)
	f.Write(line)
	f.Write([]byte("\n"))
	f.Close()

	msgs, _, err := readMessagesSince(inbox, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 valid message (malformed skipped), got %d", len(msgs))
	}
}

func TestReadInboxUnreadWithExistingCursor(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send two messages, mark read, then send one more
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "old1",
	})
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "old2",
	})

	// Mark read sets cursor
	d.ReadInbox("agent", ReadOpts{MarkRead: true})

	// Send new message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "new1",
	})

	// Read with Unread=true — should only get new message
	msgs, err := d.ReadInbox("agent", ReadOpts{Unread: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 unread message, got %d", len(msgs))
	}
	if len(msgs) > 0 && msgs[0].Body != "new1" {
		t.Errorf("expected body=new1, got %s", msgs[0].Body)
	}
}

func TestMatchFilterSince(t *testing.T) {
	opts := ReadOpts{Since: time.Now().Add(1 * time.Hour)}
	msg := core.Message{
		TS: time.Now().UTC().Format(time.RFC3339),
	}
	// Message timestamp is before Since — should not match
	if opts.match(msg) {
		t.Error("expected no match for message before Since")
	}

	// Message with bad timestamp — should not match
	msgBad := core.Message{TS: "not-a-date"}
	if opts.match(msgBad) {
		t.Error("expected no match for bad timestamp")
	}
}

func TestListReservationsMissingDir(t *testing.T) {
	root := t.TempDir()
	d, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "reservations"))

	list, err := d.ListReservations()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}
}

func TestListCommandsMissingDir(t *testing.T) {
	root := t.TempDir()
	d, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "commands"))

	cmds, err := d.ListCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected 0, got %d", len(cmds))
	}
}

func TestMatchAllFilters(t *testing.T) {
	now := time.Now().UTC()
	msg := core.Message{
		From:   "sender",
		Thread: "t1",
		TS:     now.Format(time.RFC3339),
		Type:   core.TypeAlert,
	}

	// Match all — all filters match
	opts := ReadOpts{
		From:   "sender",
		Thread: "t1",
		Since:  now.Add(-1 * time.Hour),
		Type:   core.TypeAlert,
	}
	if !opts.match(msg) {
		t.Error("expected match")
	}

	// Fail on From
	opts2 := ReadOpts{From: "other"}
	if opts2.match(msg) {
		t.Error("expected no match on From")
	}

	// Fail on Thread
	opts3 := ReadOpts{Thread: "other-thread"}
	if opts3.match(msg) {
		t.Error("expected no match on Thread")
	}

	// Fail on Type
	opts4 := ReadOpts{Type: core.TypeChat}
	if opts4.match(msg) {
		t.Error("expected no match on Type")
	}
}

func TestTouchWakeEmptyAndNonempty(t *testing.T) {
	d := tempDir(t)

	// Without text
	if err := d.TouchWake(""); err != nil {
		t.Fatal(err)
	}
	trigger := filepath.Join(d.Root, "wake", "trigger")
	if _, err := os.Stat(trigger); err != nil {
		t.Error("trigger not created")
	}

	// With text
	if err := d.TouchWake("hello"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(d.Root, "wake", "last-message"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Errorf("unexpected message: %q", data)
	}
}

func TestNewWithExistingDirs(t *testing.T) {
	// New should succeed even if dirs already exist
	root := t.TempDir()
	d1, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := New(root) // second call
	if err != nil {
		t.Fatal(err)
	}
	if d1.Root != d2.Root {
		t.Error("expected same root")
	}
}

func TestWatchInboxWithExistingMessages(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	d.Register(core.AgentMeta{Name: "bob", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send a message before watching (pre-existing)
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "existing",
	})

	type result struct {
		msgs []core.Message
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// Watch with offset 0 — should skip existing and wait for new
		msgs, _, err := d.WatchInbox("alice", 0)
		ch <- result{msgs: msgs, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	// Send new message that watcher should pick up
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "new-msg",
	})

	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("watch error: %v", got.err)
		}
		if len(got.msgs) != 1 {
			t.Fatalf("expected 1 new message, got %d", len(got.msgs))
		}
		if got.msgs[0].Body != "new-msg" {
			t.Errorf("expected body=new-msg, got %s", got.msgs[0].Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch timed out")
	}
}

func TestWatchInboxWithNonZeroOffset(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})
	d.Register(core.AgentMeta{Name: "bob", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send initial message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "first",
	})

	inbox := filepath.Join(d.AgentDir("alice"), "inbox.jsonl")
	info, _ := os.Stat(inbox)
	offset := info.Size()

	type result struct {
		msgs []core.Message
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		msgs, _, err := d.WatchInbox("alice", offset)
		ch <- result{msgs: msgs, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "second",
	})

	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("watch error: %v", got.err)
		}
		if len(got.msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(got.msgs))
		}
		if got.msgs[0].Body != "second" {
			t.Errorf("expected body=second, got %s", got.msgs[0].Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch timed out")
	}
}

func TestGCAgentWithNoHeartbeat(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Remove the heartbeat file — GC should skip (continue) this agent
	os.Remove(filepath.Join(d.AgentDir("agent"), "heartbeat"))

	result, err := d.GC(1*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	// Agent should be skipped, not counted as stale
	if result.StaleAgents != 0 {
		t.Errorf("expected 0 stale agents (no heartbeat), got %d", result.StaleAgents)
	}
}

func TestGCAgentRecentHeartbeat(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "active", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Agent has a fresh heartbeat — should not be marked stale
	result, err := d.GC(30*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.StaleAgents != 0 {
		t.Errorf("expected 0 stale agents (fresh heartbeat), got %d", result.StaleAgents)
	}
}

func TestReserveAndRelistConflict(t *testing.T) {
	d := tempDir(t)
	// Reserve twice with same params — second should fail with conflict message containing agent name
	res := core.Reservation{
		ID: core.NewULID(), Agent: "agent-1", Pattern: "file.go", Repo: "/r",
		Exclusive: true, Reason: "testing",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if err := d.Reserve(res); err != nil {
		t.Fatal(err)
	}

	res.ID = core.NewULID()
	err := d.Reserve(res)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	// Should include agent name in conflict message
	if !strings.Contains(err.Error(), "agent-1") {
		t.Errorf("conflict error should mention agent, got: %v", err)
	}
}

func TestMetricsStaleAgentNoHeartbeat(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "ghost", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Remove heartbeat — ReadHeartbeatTime should fail, agent counts as stale
	os.Remove(filepath.Join(d.AgentDir("ghost"), "heartbeat"))

	m, err := d.Metrics(5 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if m.Agents != 1 {
		t.Errorf("expected 1 agent, got %d", m.Agents)
	}
	if m.StaleAgents != 1 {
		t.Errorf("expected 1 stale (no heartbeat), got %d", m.StaleAgents)
	}
}

func TestNewFailsOnInvalidRoot(t *testing.T) {
	// Use a file as root — MkdirAll should fail
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "not-a-dir")
	os.WriteFile(filePath, []byte("x"), 0644)

	_, err := New(filePath)
	if err == nil {
		t.Error("expected error when root is a file")
	}
}

func TestConsumeCommandMissingCommandsDir(t *testing.T) {
	d := tempDir(t)
	os.RemoveAll(filepath.Join(d.Root, "commands"))

	_, err := d.ConsumeCommand("test-id")
	if err == nil {
		t.Error("expected error when commands dir is missing")
	}
}

func TestSendSubjectTruncation(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "target", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	longBody := strings.Repeat("x", 100)
	msg := core.Message{
		From: "sender", To: "target", Body: longBody,
	}
	if err := d.Send(msg); err != nil {
		t.Fatal(err)
	}

	msgs, err := d.ReadInbox("target", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}
	if len(msgs[0].Subject) > 80 {
		t.Errorf("subject should be truncated to 80, got %d", len(msgs[0].Subject))
	}
}

// --- Tests that matter: real agent failure modes ---

// TestSendReadRoundtripPreservesAllFields verifies every Message field survives
// the send→inbox→read roundtrip. Agents depend on all fields being intact.
func TestSendReadRoundtripPreservesAllFields(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	ts := time.Now().UTC().Format(time.RFC3339)
	sent := core.Message{
		ID:       core.NewULID(),
		TS:       ts,
		From:     "bob",
		To:       "alice",
		Subject:  "task complete",
		Body:     "All tests pass, auth refactor merged.",
		Thread:   "br-42",
		Priority: "high",
		ReplyTo:  "prev-msg-id",
		Tags:     []string{"auth", "release"},
		Type:     core.TypeTaskResult,
		Payload:  json.RawMessage(`{"exit_code":0,"files_changed":3}`),
	}
	if err := d.Send(sent); err != nil {
		t.Fatal(err)
	}

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	got := msgs[0]

	if got.ID != sent.ID {
		t.Errorf("ID: got %q, want %q", got.ID, sent.ID)
	}
	if got.TS != sent.TS {
		t.Errorf("TS: got %q, want %q", got.TS, sent.TS)
	}
	if got.From != sent.From {
		t.Errorf("From: got %q, want %q", got.From, sent.From)
	}
	if got.To != sent.To {
		t.Errorf("To: got %q, want %q", got.To, sent.To)
	}
	if got.Subject != sent.Subject {
		t.Errorf("Subject: got %q, want %q", got.Subject, sent.Subject)
	}
	if got.Body != sent.Body {
		t.Errorf("Body: got %q, want %q", got.Body, sent.Body)
	}
	if got.Thread != sent.Thread {
		t.Errorf("Thread: got %q, want %q", got.Thread, sent.Thread)
	}
	if got.Priority != sent.Priority {
		t.Errorf("Priority: got %q, want %q", got.Priority, sent.Priority)
	}
	if got.ReplyTo != sent.ReplyTo {
		t.Errorf("ReplyTo: got %q, want %q", got.ReplyTo, sent.ReplyTo)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "auth" || got.Tags[1] != "release" {
		t.Errorf("Tags: got %v, want [auth release]", got.Tags)
	}
	if got.Type != sent.Type {
		t.Errorf("Type: got %q, want %q", got.Type, sent.Type)
	}
	if string(got.Payload) != string(sent.Payload) {
		t.Errorf("Payload: got %s, want %s", got.Payload, sent.Payload)
	}
}

// TestConcurrentReserveSamePattern exercises the O_CREAT|O_EXCL race.
// When two agents try to reserve the same pattern simultaneously,
// exactly one must win and the other must get a conflict error.
func TestConcurrentReserveSamePattern(t *testing.T) {
	d := tempDir(t)

	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res := core.Reservation{
				ID:        core.NewULID(),
				Agent:     fmt.Sprintf("agent-%d", n),
				Pattern:   "contested.go",
				Repo:      "/repo",
				Exclusive: true,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}
			results <- d.Reserve(res)
		}(i)
	}
	wg.Wait()
	close(results)

	wins := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			wins++
		} else if strings.Contains(err.Error(), "conflict") {
			conflicts++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("exactly 1 goroutine should win the reservation, got %d", wins)
	}
	if conflicts != 9 {
		t.Errorf("expected 9 conflicts, got %d", conflicts)
	}
}

// TestInboxCorruptionMidFileRecovery verifies that a corrupt JSON line
// between valid messages doesn't lose the valid ones. This simulates
// disk corruption or a partial write from a killed process.
func TestInboxCorruptionMidFileRecovery(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send first valid message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "first-valid",
	})

	// Inject corrupt line directly into inbox
	inbox := filepath.Join(d.AgentDir("alice"), "inbox.jsonl")
	f, err := os.OpenFile(inbox, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("{\"id\":\"corrupt\",\"body\":garbled nonsense}\n"))
	f.Close()

	// Send second valid message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "bob", To: "alice", Body: "second-valid",
	})

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 valid messages (corrupt line skipped), got %d", len(msgs))
	}
	if msgs[0].Body != "first-valid" {
		t.Errorf("first message body: got %q, want %q", msgs[0].Body, "first-valid")
	}
	if msgs[1].Body != "second-valid" {
		t.Errorf("second message body: got %q, want %q", msgs[1].Body, "second-valid")
	}
}

// TestGCPreservesActiveReservations verifies GC only removes expired
// reservations and does not touch active ones — a critical safety property.
func TestGCPreservesActiveReservations(t *testing.T) {
	d := tempDir(t)

	active := core.Reservation{
		ID: core.NewULID(), Agent: "agent-1", Pattern: "active.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	expired := core.Reservation{
		ID: core.NewULID(), Agent: "agent-2", Pattern: "old.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	}
	d.Reserve(active)
	d.Reserve(expired)

	result, err := d.GC(30*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredReservations != 1 {
		t.Errorf("expected 1 expired removed, got %d", result.ExpiredReservations)
	}

	remaining, _ := d.ListReservations()
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining reservation, got %d", len(remaining))
	}
	if remaining[0].Pattern != "active.go" {
		t.Errorf("surviving reservation should be active.go, got %s", remaining[0].Pattern)
	}
}

// TestSubjectAutoTruncationBoundary documents the exact 80-char truncation
// boundary for auto-generated subjects. Agents depend on this contract.
func TestSubjectAutoTruncationBoundary(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "target", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	cases := []struct {
		bodyLen       int
		wantSubjectLen int
	}{
		{79, 79},  // under limit: subject == body
		{80, 80},  // exactly at limit: subject == body
		{81, 80},  // over limit: subject truncated to 80
		{200, 80}, // well over: truncated to 80
	}
	for _, tc := range cases {
		body := strings.Repeat("a", tc.bodyLen)
		msg := core.Message{
			From: "sender", To: "target", Body: body,
		}
		if err := d.Send(msg); err != nil {
			t.Fatalf("send body len %d: %v", tc.bodyLen, err)
		}
	}

	msgs, err := d.ReadInbox("target", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != len(cases) {
		t.Fatalf("expected %d messages, got %d", len(cases), len(msgs))
	}
	for i, tc := range cases {
		if len(msgs[i].Subject) != tc.wantSubjectLen {
			t.Errorf("body len %d: subject len = %d, want %d", tc.bodyLen, len(msgs[i].Subject), tc.wantSubjectLen)
		}
		// Body should never be truncated
		if len(msgs[i].Body) != tc.bodyLen {
			t.Errorf("body len %d: body was modified to len %d", tc.bodyLen, len(msgs[i].Body))
		}
	}
}

// TestReleaseByWrongAgentReportsOwner verifies the error message includes
// the actual owner's name. Agents parse these errors to understand conflicts.
func TestReleaseByWrongAgentReportsOwner(t *testing.T) {
	d := tempDir(t)
	d.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "rightful-owner", Pattern: "file.go", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})

	err := d.Release("intruder", "/r", "file.go")
	if err == nil {
		t.Fatal("expected error for wrong agent")
	}
	if !strings.Contains(err.Error(), "rightful-owner") {
		t.Errorf("error should mention the actual owner 'rightful-owner', got: %v", err)
	}
	if !strings.Contains(err.Error(), "intruder") {
		t.Errorf("error should mention the requesting agent 'intruder', got: %v", err)
	}
}

// TestCheckOverlapIgnoresExpiredReservations verifies that expired reservations
// are not reported as conflicts. Without this, agents would be blocked by
// ghost reservations from crashed agents.
func TestCheckOverlapIgnoresExpiredReservations(t *testing.T) {
	d := tempDir(t)

	// Create an expired reservation
	d.Reserve(core.Reservation{
		ID: core.NewULID(), Agent: "dead-agent", Pattern: "src/**", Repo: "/r",
		Exclusive: true,
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})

	// New agent checking overlap should get zero conflicts
	conflicts, err := d.CheckOverlap("new-agent", "/r", "src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts (expired reservation), got %d", len(conflicts))
	}
}

// TestReadUnreadFirstTimeNoCursor exercises the first --unread read on a
// fresh agent that has never read before (no cursor file exists).
// This is the most common agent startup path.
func TestReadUnreadFirstTimeNoCursor(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Send a message
	d.Send(core.Message{
		ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
		From: "sender", To: "agent", Body: "hello",
	})

	// First unread read — no cursor file exists
	// Should return all messages (cursor defaults to 0)
	msgs, err := d.ReadInbox("agent", ReadOpts{Unread: true})
	if err != nil {
		t.Fatalf("first unread read failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message on first unread read, got %d", len(msgs))
	}
}

// TestMessageOrderingPreserved verifies that messages from sequential sends
// appear in chronological order. Agents depend on this for conversation flow.
func TestMessageOrderingPreserved(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "alice", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	for i := 0; i < 10; i++ {
		d.Send(core.Message{
			ID: core.NewULID(), TS: time.Now().UTC().Format(time.RFC3339),
			From: "bob", To: "alice", Body: fmt.Sprintf("msg-%02d", i),
		})
	}

	msgs, err := d.ReadInbox("alice", ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(msgs)-1; i++ {
		if msgs[i].Body >= msgs[i+1].Body {
			t.Errorf("messages out of order: [%d]=%s >= [%d]=%s",
				i, msgs[i].Body, i+1, msgs[i+1].Body)
		}
	}
}

// TestGCStaleAgentRenamesHeartbeat verifies that GC marks stale agents by
// renaming heartbeat→heartbeat.stale. This is how Athena detects dead agents.
func TestGCStaleAgentRenamesHeartbeat(t *testing.T) {
	d := tempDir(t)
	d.Register(core.AgentMeta{Name: "stale-agent", RegisteredAt: time.Now().UTC().Format(time.RFC3339)})

	// Backdate heartbeat
	hbPath := filepath.Join(d.AgentDir("stale-agent"), "heartbeat")
	old := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	os.WriteFile(hbPath, []byte(old+"\n"), 0644)

	result, err := d.GC(5*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.StaleAgents != 1 {
		t.Errorf("expected 1 stale agent, got %d", result.StaleAgents)
	}

	// heartbeat should be renamed to heartbeat.stale
	if _, err := os.Stat(hbPath); !os.IsNotExist(err) {
		t.Error("heartbeat file should have been renamed")
	}
	stalePath := filepath.Join(d.AgentDir("stale-agent"), "heartbeat.stale")
	if _, err := os.Stat(stalePath); err != nil {
		t.Errorf("heartbeat.stale should exist: %v", err)
	}
}
