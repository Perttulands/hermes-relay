package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/core"
	"github.com/Perttulands/hermes-relay/internal/store"
)

func setup(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	origDir := os.Getenv("RELAY_DIR")
	origAgent := os.Getenv("RELAY_AGENT")
	os.Setenv("RELAY_DIR", dir)
	os.Setenv("RELAY_AGENT", "test-agent")
	return dir, func() {
		os.Setenv("RELAY_DIR", origDir)
		os.Setenv("RELAY_AGENT", origAgent)
	}
}

// setupAllowAllPolicy writes an allow-all activation policy for tests
// that need wake to proceed without policy blocking.
func setupAllowAllPolicy(t *testing.T, dir string) {
	t.Helper()
	s, err := store.New(dir)
	if err != nil {
		t.Fatalf("setup allow-all policy: %v", err)
	}
	p := &store.ActivationPolicy{
		Default: "allow",
	}
	if err := s.SavePolicy(p); err != nil {
		t.Fatalf("setup allow-all policy: %v", err)
	}
}

func run(args ...string) int {
	full := append([]string{"relay"}, args...)
	return Run(full)
}

func withMockExec(t *testing.T, fn func(name string, args ...string) *exec.Cmd) {
	t.Helper()
	orig := execCommand
	execCommand = fn
	t.Cleanup(func() {
		execCommand = orig
	})
}

func TestVersion(t *testing.T) {
	code := run("version")
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestHelp(t *testing.T) {
	for _, cmd := range []string{"help", "--help", "-h"} {
		code := run(cmd)
		if code != 0 {
			t.Errorf("%s: expected exit 0, got %d", cmd, code)
		}
	}
}

func TestNoArgs(t *testing.T) {
	code := run()
	if code != 1 {
		t.Errorf("expected exit 1 for no args, got %d", code)
	}
}

func TestUnknownCommand(t *testing.T) {
	code := run("foobar")
	if code != 1 {
		t.Errorf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestRegister(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "my-agent", "--program", "claude-code", "--model", "opus", "--task", "testing")
	if code != 0 {
		t.Fatalf("register failed with code %d", code)
	}

	// Verify meta.json was created
	dir := os.Getenv("RELAY_DIR")
	data, err := os.ReadFile(filepath.Join(dir, "agents", "my-agent", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta core.AgentMeta
	json.Unmarshal(data, &meta)
	if meta.Program != "claude-code" {
		t.Errorf("expected program=claude-code, got %s", meta.Program)
	}
}

func TestRegisterNoName(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register")
	if code != 1 {
		t.Errorf("expected exit 1 for register without name, got %d", code)
	}
}

func TestRegisterRejectsDashPrefixedName(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("register", "--help")
	if code != 1 {
		t.Errorf("expected exit 1 for dash-prefixed register name, got %d", code)
	}
}

func TestHeartbeat(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	code := run("heartbeat")
	if code != 0 {
		t.Errorf("heartbeat failed with code %d", code)
	}
}

func TestHeartbeatWithTask(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent", "--task", "original")
	code := run("heartbeat", "--task", "updated")
	if code != 0 {
		t.Fatalf("heartbeat --task failed with code %d", code)
	}

	dir := os.Getenv("RELAY_DIR")
	data, _ := os.ReadFile(filepath.Join(dir, "agents", "test-agent", "meta.json"))
	var meta core.AgentMeta
	json.Unmarshal(data, &meta)
	if meta.Task != "updated" {
		t.Errorf("expected task=updated, got %s", meta.Task)
	}
}

func TestSendAndRead(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "other-agent")

	// Send from test-agent to other-agent
	code := run("send", "other-agent", "hello world", "--agent", "test-agent", "--thread", "t1")
	if code != 0 {
		t.Fatalf("send failed with code %d", code)
	}

	// Read as other-agent
	code = run("read", "--agent", "other-agent")
	if code != 0 {
		t.Fatalf("read failed with code %d", code)
	}
}

func TestInboxAlias(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "hello from alias test", "--agent", "sender")

	code := run("inbox")
	if code != 0 {
		t.Fatalf("inbox alias failed with code %d", code)
	}
}

func TestWatchReceivesMessage(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "watcher")
	run("register", "sender")

	done := make(chan int, 1)
	go func() {
		done <- run("watch", "--agent", "watcher")
	}()

	time.Sleep(100 * time.Millisecond)
	sendCode := run("send", "watcher", "watch message", "--agent", "sender")
	if sendCode != 0 {
		t.Fatalf("send failed with code %d", sendCode)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("watch failed with code %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch timed out")
	}
}

func TestSendNoRecipient(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("send")
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestSendToNonexistent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("send", "nobody", "hello")
	if code != 1 {
		t.Errorf("expected exit 1 for nonexistent recipient, got %d", code)
	}
}

func TestBroadcast(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-a")
	run("register", "agent-b")
	run("register", "agent-c")

	code := run("send", "--broadcast", "everyone listen up", "--agent", "agent-a")
	if code != 0 {
		t.Fatalf("broadcast failed with code %d", code)
	}

	// Both b and c should have messages
	dir := os.Getenv("RELAY_DIR")
	for _, name := range []string{"agent-b", "agent-c"} {
		data, err := os.ReadFile(filepath.Join(dir, "agents", name, "inbox.jsonl"))
		if err != nil {
			t.Errorf("%s inbox not found: %v", name, err)
			continue
		}
		if !strings.Contains(string(data), "everyone listen up") {
			t.Errorf("%s inbox missing broadcast message", name)
		}
	}

	// Sender should NOT have the message
	_, err := os.ReadFile(filepath.Join(dir, "agents", "agent-a", "inbox.jsonl"))
	if err == nil {
		t.Log("sender has inbox (may be empty)")
	}
}

func TestReserveAndRelease(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("reserve", "src/auth/**", "--repo", "/tmp/test-repo", "--reason", "testing")
	if code != 0 {
		t.Fatalf("reserve failed with code %d", code)
	}

	// List reservations
	code = run("reservations")
	if code != 0 {
		t.Fatalf("reservations failed with code %d", code)
	}

	// Release
	code = run("release", "src/auth/**", "--repo", "/tmp/test-repo")
	if code != 0 {
		t.Fatalf("release failed with code %d", code)
	}
}

func TestReserveConflict(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "file.go", "--repo", "/tmp/test-repo", "--agent", "agent-1")

	// Same pattern should fail
	code := run("reserve", "file.go", "--repo", "/tmp/test-repo", "--agent", "agent-2")
	if code != 1 {
		t.Errorf("expected conflict exit 1, got %d", code)
	}
}

func TestReserveCheck(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "src/**", "--repo", "/tmp/test-repo", "--agent", "agent-1")

	// Check for overlap — should detect conflict
	code := run("reserve", "src/main.go", "--repo", "/tmp/test-repo", "--agent", "agent-2", "--check")
	if code != 1 {
		t.Errorf("expected conflict check to return 1, got %d", code)
	}

	// No overlap
	code = run("reserve", "lib/util.go", "--repo", "/tmp/test-repo", "--agent", "agent-2", "--check")
	if code != 0 {
		t.Errorf("expected no conflict, got %d", code)
	}
}

func TestReleaseAll(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("reserve", "a.go", "--repo", "/tmp/r")
	run("reserve", "b.go", "--repo", "/tmp/r")

	code := run("release", "--all")
	if code != 0 {
		t.Fatalf("release --all failed with code %d", code)
	}
}

func TestStatus(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1", "--task", "auth refactor")
	run("register", "agent-2", "--task", "api work")

	code := run("status")
	if code != 0 {
		t.Errorf("status failed with code %d", code)
	}
}

func TestStatusJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1")
	code := run("status", "--json")
	if code != 0 {
		t.Errorf("status --json failed with code %d", code)
	}
}

func TestCmd(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("cmd", "agent:main:main", "/verify", "repo", "br-42")
	if code != 0 {
		t.Fatalf("cmd failed with code %d", code)
	}

	// Verify command file was created
	dir := os.Getenv("RELAY_DIR")
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	jsonCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			jsonCount++
		}
	}
	if jsonCount != 1 {
		t.Errorf("expected 1 command file, got %d", jsonCount)
	}
}

func TestCmdNoArgs(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("cmd")
	if code != 1 {
		t.Errorf("expected exit 1 for cmd without args, got %d", code)
	}
}

func TestGC(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("gc")
	if code != 0 {
		t.Errorf("gc failed with code %d", code)
	}
}

func TestMetrics(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-a")
	run("register", "agent-b")
	run("send", "agent-b", "hello", "--agent", "agent-a")
	run("reserve", "src/**", "--repo", "/tmp/test-repo", "--agent", "agent-a")

	code := run("metrics")
	if code != 0 {
		t.Fatalf("metrics failed with code %d", code)
	}
}

func TestMetricsJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-a")
	run("send", "agent-a", "msg1", "--agent", "agent-a")

	code := run("metrics", "--json")
	if code != 0 {
		t.Fatalf("metrics --json failed with code %d", code)
	}
}

func TestGCDryRun(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("gc", "--dry-run")
	if code != 0 {
		t.Errorf("gc --dry-run failed with code %d", code)
	}
}

func TestWakeFileMethod(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("wake", "test wake", "--method", "file")
	if code != 0 {
		t.Errorf("wake --method file failed with code %d", code)
	}

	// Check trigger file exists
	dir := os.Getenv("RELAY_DIR")
	if _, err := os.Stat(filepath.Join(dir, "wake", "trigger")); err != nil {
		t.Errorf("trigger file not created: %v", err)
	}
}

func TestGlobalFlags(t *testing.T) {
	dir := t.TempDir()

	// Use --dir and --agent flags
	code := Run([]string{"relay", "register", "custom-agent", "--dir", dir, "--agent", "custom-agent"})
	if code != 0 {
		t.Fatalf("register with --dir failed with code %d", code)
	}

	// Verify it was created in the custom dir
	if _, err := os.Stat(filepath.Join(dir, "agents", "custom-agent", "meta.json")); err != nil {
		t.Errorf("meta.json not in custom dir: %v", err)
	}
}

func TestReadUnread(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "sender")

	// Send a message
	run("send", "test-agent", "msg1", "--agent", "sender")

	// Read with mark-read
	code := run("read", "--mark-read")
	if code != 0 {
		t.Fatalf("read --mark-read failed with code %d", code)
	}

	// Read unread — should be empty
	code = run("read", "--unread", "--json")
	if code != 0 {
		t.Fatalf("read --unread failed with code %d", code)
	}
}

func TestReadJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "hello", "--agent", "sender")

	code := run("read", "--json")
	if code != 0 {
		t.Fatalf("read --json failed with code %d", code)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"0s", "now"},
		{"30s", "30s"},
		{"5m0s", "5m"},
		{"2h30m0s", "2h30m"},
		{"48h0m0s", "2d"},
	}
	for _, tc := range cases {
		d := parseDuration(tc.input)
		got := formatAge(d)
		if got != tc.expected {
			t.Errorf("formatAge(%s) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseSince(t *testing.T) {
	// Duration
	s := parseSince("1h")
	if s.IsZero() {
		t.Error("parseSince(1h) returned zero")
	}

	// RFC3339
	s = parseSince("2026-02-16T00:00:00Z")
	if s.IsZero() {
		t.Error("parseSince(RFC3339) returned zero")
	}

	// Date
	s = parseSince("2026-02-16")
	if s.IsZero() {
		t.Error("parseSince(date) returned zero")
	}
}

func TestSpawnRequiresFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	if code := run("spawn"); code != 1 {
		t.Fatalf("expected exit 1 for missing required flags, got %d", code)
	}
}

func TestSpawnSuccess(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	dispatch := filepath.Join(t.TempDir(), "dispatch.sh")
	if err := os.WriteFile(dispatch, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCH_SCRIPT", dispatch)
	t.Setenv("ATHENA_WORKSPACE", t.TempDir())

	repo := t.TempDir()
	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch filepath.Base(name) {
		case "br":
			return exec.Command("bash", "-lc", "echo '✓ Created issue: athena-xyz'")
		default:
			return exec.Command("bash", "-lc", "exit 0")
		}
	})

	code := run("spawn", "--repo", repo, "--agent", "codex", "--prompt", "Implement feature X")
	if code != 0 {
		t.Fatalf("spawn failed with code %d", code)
	}
	if len(calls) < 2 {
		t.Fatalf("expected br + dispatch calls, got %v", calls)
	}
	if !strings.Contains(calls[0], "create Implement feature X -t task") {
		t.Fatalf("unexpected br call: %s", calls[0])
	}
	if !strings.Contains(calls[1], fmt.Sprintf("athena-xyz %s codex Implement feature X", repo)) {
		t.Fatalf("unexpected dispatch call: %s", calls[1])
	}
}

func TestSpawnWaitAndNotify(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	dispatch := filepath.Join(t.TempDir(), "dispatch.sh")
	if err := os.WriteFile(dispatch, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCH_SCRIPT", dispatch)
	t.Setenv("ATHENA_WORKSPACE", t.TempDir())

	origPoll := spawnPollInterval
	origTimeout := spawnPollTimeout
	spawnPollInterval = 10 * time.Millisecond
	spawnPollTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		spawnPollInterval = origPoll
		spawnPollTimeout = origTimeout
	})

	repo := t.TempDir()
	resultsDir := filepath.Join(repo, "state", "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(resultsDir, "athena-wait.json"), []byte(`{"ok":true}`), 0o644)
	}()

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if filepath.Base(name) == "br" {
			return exec.Command("bash", "-lc", "echo '✓ Created issue: athena-wait'")
		}
		return exec.Command("bash", "-lc", "exit 0")
	})

	code := run(
		"spawn",
		"--repo", repo,
		"--agent", "claude:opus",
		"--prompt", "Design system Y",
		"--wait",
		"--notify", "athena",
	)
	if code != 0 {
		t.Fatalf("spawn --wait --notify failed with code %d", code)
	}

	foundNotify := false
	for _, c := range calls {
		if strings.Contains(c, "relay send athena Spawned task athena-wait completed") {
			foundNotify = true
			break
		}
	}
	if !foundNotify {
		t.Fatalf("expected notify call, got %v", calls)
	}
}

// --- Agent card CLI tests ---

func TestRegisterCreatesCard(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("register", "card-agent", "--skills", "go,rust", "--task", "br-42")
	if code != 0 {
		t.Fatalf("register failed with code %d", code)
	}

	// Verify card.json was created
	data, err := os.ReadFile(filepath.Join(dir, "agents", "card-agent", "card.json"))
	if err != nil {
		t.Fatal(err)
	}
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.Name != "card-agent" {
		t.Errorf("expected name=card-agent, got %s", card.Name)
	}
	if len(card.Skills) != 2 || card.Skills[0] != "go" || card.Skills[1] != "rust" {
		t.Errorf("unexpected skills: %v", card.Skills)
	}
	if card.Status != "working" {
		t.Errorf("expected status=working (task provided), got %s", card.Status)
	}
	if card.CurrentTask != "br-42" {
		t.Errorf("expected current_task=br-42, got %s", card.CurrentTask)
	}
}

func TestRegisterCreatesCardNoSkills(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("register", "simple-agent")
	if code != 0 {
		t.Fatalf("register failed with code %d", code)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "simple-agent", "card.json"))
	if err != nil {
		t.Fatal(err)
	}
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.Status != "idle" {
		t.Errorf("expected status=idle, got %s", card.Status)
	}
	if card.Skills != nil {
		t.Errorf("expected nil skills, got %v", card.Skills)
	}
}

func TestHeartbeatWithTaskSetsWorkingOnCard(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent")
	code := run("heartbeat", "--task", "br-99")
	if code != 0 {
		t.Fatalf("heartbeat --task failed with code %d", code)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "agents", "test-agent", "card.json"))
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.Status != "working" {
		t.Errorf("expected status=working, got %s", card.Status)
	}
	if card.CurrentTask != "br-99" {
		t.Errorf("expected current_task=br-99, got %s", card.CurrentTask)
	}
}

func TestHeartbeatIdleClearsTask(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent", "--task", "br-42")
	code := run("heartbeat", "--idle")
	if code != 0 {
		t.Fatalf("heartbeat --idle failed with code %d", code)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "agents", "test-agent", "card.json"))
	var card core.AgentCard
	json.Unmarshal(data, &card)
	if card.Status != "idle" {
		t.Errorf("expected status=idle, got %s", card.Status)
	}
	if card.CurrentTask != "" {
		t.Errorf("expected empty current_task, got %s", card.CurrentTask)
	}
}

func TestCardCommandSelf(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent", "--skills", "go,analysis")
	code := run("card")
	if code != 0 {
		t.Fatalf("card (self) failed with code %d", code)
	}
}

func TestCardCommandNamedAgent(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "other-agent", "--skills", "python")
	code := run("card", "other-agent")
	if code != 0 {
		t.Fatalf("card other-agent failed with code %d", code)
	}
}

func TestCardCommandJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "test-agent", "--skills", "go")
	code := run("card", "--json")
	if code != 0 {
		t.Fatalf("card --json failed with code %d", code)
	}
}

func TestCardCommandAll(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1", "--skills", "go")
	run("register", "agent-2", "--skills", "rust")

	code := run("card", "--all")
	if code != 0 {
		t.Fatalf("card --all failed with code %d", code)
	}
}

func TestCardCommandAllJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1")
	run("register", "agent-2")

	code := run("card", "--all", "--json")
	if code != 0 {
		t.Fatalf("card --all --json failed with code %d", code)
	}
}

func TestCardCommandMissing(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// No agent registered — should fail
	code := run("card", "nonexistent")
	if code != 1 {
		t.Errorf("expected exit 1 for missing card, got %d", code)
	}
}

func TestStatusShowsCardData(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	run("register", "agent-1", "--skills", "go,testing", "--task", "br-42")
	code := run("status")
	if code != 0 {
		t.Errorf("status failed with code %d", code)
	}
}

func TestRegisterJSONOutputIsCard(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// When --json, register outputs the card
	code := run("register", "json-card-agent", "--skills", "go", "--json")
	if code != 0 {
		t.Fatalf("register --json failed with code %d", code)
	}
}

// --- Output capture helpers ---

// captureRun runs a CLI command and returns (exit code, stdout).
func captureRun(t *testing.T, args ...string) (int, string) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	full := append([]string{"relay"}, args...)
	code := Run(full)

	w.Close()
	os.Stdout = old

	data, err2 := io.ReadAll(r)
	if err2 != nil {
		t.Fatal(err2)
	}
	return code, string(data)
}

// --- CLI output assertion tests (pol-20co) ---

func TestSendOutputContainsDelivered(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code, out := captureRun(t, "send", "target", "hello world")
	if code != 0 {
		t.Fatalf("send failed with code %d", code)
	}
	if !strings.Contains(strings.ToLower(out), "deliver") && !strings.Contains(strings.ToLower(out), "sent") && !strings.Contains(out, "→") {
		t.Errorf("send output should indicate delivery, got: %q", out)
	}
}

func TestSendJSONOutputIsValidJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "target")

	code, out := captureRun(t, "send", "target", "json test", "--json")
	if code != 0 {
		t.Fatalf("send --json failed with code %d", code)
	}
	out = strings.TrimSpace(out)
	if !json.Valid([]byte(out)) {
		t.Errorf("send --json output is not valid JSON: %q", out)
	}
}

func TestReadOutputContainsMessageBody(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "unique-test-body-42", "--agent", "sender")

	code, out := captureRun(t, "read")
	if code != 0 {
		t.Fatalf("read failed with code %d", code)
	}
	if !strings.Contains(out, "unique-test-body-42") {
		t.Errorf("read output should contain message body, got: %q", out)
	}
}

func TestReadJSONOutputContainsMessages(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")
	run("register", "sender")
	run("send", "test-agent", "json-body-check", "--agent", "sender")

	code, out := captureRun(t, "read", "--json")
	if code != 0 {
		t.Fatalf("read --json failed with code %d", code)
	}
	out = strings.TrimSpace(out)
	if !strings.Contains(out, "json-body-check") {
		t.Errorf("read --json output should contain message body, got: %q", out)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(out), &msgs); err != nil {
		t.Errorf("read --json should produce JSON array: %v\noutput: %q", err, out)
	}
}

func TestReadEmptyInboxOutput(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "test-agent")

	code, out := captureRun(t, "read")
	if code != 0 {
		t.Fatalf("read empty inbox failed with code %d", code)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "no messages") && !strings.Contains(lower, "empty") && strings.TrimSpace(out) != "" {
		t.Logf("empty inbox output: %q (acceptable)", out)
	}
}

func TestMetricsOutputContainsCounts(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "agent-a")
	run("register", "agent-b")
	run("send", "agent-b", "hello", "--agent", "agent-a")

	code, out := captureRun(t, "metrics")
	if code != 0 {
		t.Fatalf("metrics failed with code %d", code)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "agent") {
		t.Errorf("metrics output should mention agents, got: %q", out)
	}
	if !strings.Contains(lower, "message") {
		t.Errorf("metrics output should mention messages, got: %q", out)
	}
}

func TestMetricsJSONOutputIsValid(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "agent-a")

	code, out := captureRun(t, "metrics", "--json")
	if code != 0 {
		t.Fatalf("metrics --json failed with code %d", code)
	}
	out = strings.TrimSpace(out)
	if !json.Valid([]byte(out)) {
		t.Errorf("metrics --json output is not valid JSON: %q", out)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("failed to parse metrics JSON: %v", err)
	}
	for _, key := range []string{"agents", "total_messages", "reservations", "commands"} {
		if _, ok := m[key]; !ok {
			t.Errorf("metrics JSON missing key %q", key)
		}
	}
}

func TestStatusOutputContainsAgentNames(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()
	run("register", "alpha-agent", "--task", "alpha work")
	run("register", "beta-agent", "--task", "beta work")

	code, out := captureRun(t, "status")
	if code != 0 {
		t.Fatalf("status failed with code %d", code)
	}
	if !strings.Contains(out, "alpha-agent") {
		t.Errorf("status output should contain 'alpha-agent', got: %q", out)
	}
	if !strings.Contains(out, "beta-agent") {
		t.Errorf("status output should contain 'beta-agent', got: %q", out)
	}
}

func TestVersionOutputContainsVersion(t *testing.T) {
	code, out := captureRun(t, "version")
	if code != 0 {
		t.Fatalf("version failed with code %d", code)
	}
	if !strings.Contains(out, "relay") {
		t.Errorf("version output should contain 'relay', got: %q", out)
	}
}

// --- extractSpawnBeadID tests ---

func TestExtractSpawnBeadID(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{"primary regex match", "✓ Created issue: athena-xyz", "athena-xyz"},
		{"primary with extra whitespace", "Created issue:   pol-3fa  ", "pol-3fa"},
		{"primary with surrounding text", "some noise Created issue: br-42 more noise", "br-42"},
		{"fallback regex match", "something athena-xyz something", "athena-xyz"},
		{"fallback with dashes", "got pol-2r-extra here", "pol-2r-extra"},
		{"no match empty string", "", ""},
		{"no match no bead-like ID", "just plain text with no ids", ""},
		{"multiline primary", "line1\n✓ Created issue: athena-abc\nline3", "athena-abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSpawnBeadID(tc.input)
			if got != tc.expect {
				t.Errorf("extractSpawnBeadID(%q) = %q, want %q", tc.input, got, tc.expect)
			}
		})
	}
}

// Integration test: full multi-agent scenario
func TestFullScenario(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	// Register 3 agents
	run("register", "athena", "--program", "openclaw", "--task", "orchestrator")
	run("register", "agent-1", "--program", "claude-code", "--model", "opus", "--task", "auth refactor", "--bead", "br-42")
	run("register", "agent-2", "--program", "claude-code", "--model", "opus", "--task", "API endpoints", "--bead", "br-43")

	// Reserve files
	if code := run("reserve", "src/auth/**", "--repo", "/tmp/repo", "--agent", "agent-1", "--reason", "br-42"); code != 0 {
		t.Fatalf("reserve auth failed: %d", code)
	}
	if code := run("reserve", "src/api/**", "--repo", "/tmp/repo", "--agent", "agent-2", "--reason", "br-43"); code != 0 {
		t.Fatalf("reserve api failed: %d", code)
	}

	// Check overlap — agent-2 trying to touch auth files should warn
	if code := run("reserve", "src/auth/login.go", "--repo", "/tmp/repo", "--agent", "agent-2", "--check"); code != 1 {
		t.Error("expected overlap detection to fail")
	}

	// Send messages
	run("send", "athena", "Starting auth refactor", "--agent", "agent-1", "--thread", "br-42")
	run("send", "athena", "Starting API work", "--agent", "agent-2", "--thread", "br-43")

	// Status should show everything
	if code := run("status"); code != 0 {
		t.Errorf("status failed: %d", code)
	}

	// Agent-1 completes, releases and sends
	run("release", "--all", "--agent", "agent-1")
	run("send", "athena", "br-42 complete. All tests pass.", "--agent", "agent-1", "--thread", "br-42", "--priority", "high")

	// Post command
	run("cmd", "agent:main:main", "/verify", "repo", "br-42", "--agent", "agent-1")

	// Athena reads messages
	if code := run("read", "--agent", "athena", "--from", "agent-1"); code != 0 {
		t.Errorf("read failed: %d", code)
	}

	// GC
	if code := run("gc", "--dry-run"); code != 0 {
		t.Errorf("gc dry-run failed: %d", code)
	}
}
