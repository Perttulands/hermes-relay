package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/relay/internal/core"
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
	for _, sub := range []string{"agents", "reservations", "commands", "wake"} {
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
		{"src/auth/**", "src/auth/login.go", true},
		{"src/**", "src/auth/login.go", true},
		{"src/a/*", "src/b/*", false},
		{"*.go", "src/main.go", false},       // different prefix, no ** in either
		{"**", "anything", true},             // ** overlaps everything
		{"src/auth/**", "src/auth/**", true}, // exact match
		{"src/auth/**", "src/api/**", false}, // different subtrees
		{"*.go", "*.go", true},               // same pattern
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
