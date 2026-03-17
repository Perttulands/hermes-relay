package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildRelay compiles the relay binary into a temp directory and returns the path.
func buildRelay(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "relay")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/relay")
	// Resolve project root relative to this test file.
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build relay: %v\n%s", err, out)
	}
	return bin
}

func seedAllowAllPolicy(t *testing.T, dir string) {
	t.Helper()
	policyPath := filepath.Join(dir, "activation-policy.toml")
	if err := os.WriteFile(policyPath, []byte("default = \"allow\"\n"), 0o644); err != nil {
		t.Fatalf("write activation policy: %v", err)
	}
}

// runRelay executes the relay binary with the given args and env overrides.
// Returns stdout, stderr, and exit code.
func runRelay(t *testing.T, bin string, env map[string]string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec relay: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func TestE2E_HelpExitsCleanly(t *testing.T) {
	bin := buildRelay(t)

	for _, flag := range []string{"--help", "-h", "help"} {
		stdout, _, code := runRelay(t, bin, nil, flag)
		if code != 0 {
			t.Errorf("%s: expected exit 0, got %d", flag, code)
		}
		if !strings.Contains(stdout, "relay") {
			t.Errorf("%s: expected help output to mention 'relay', got: %q", flag, stdout)
		}
		if !strings.Contains(stdout, "send") {
			t.Errorf("%s: expected help output to mention 'send' command", flag)
		}
	}
}

func TestE2E_VersionExitsCleanly(t *testing.T) {
	bin := buildRelay(t)

	stdout, _, code := runRelay(t, bin, nil, "version")
	if code != 0 {
		t.Fatalf("version: expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "relay") {
		t.Errorf("version output should contain 'relay', got: %q", stdout)
	}
}

func TestE2E_SendAndRead(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	seedAllowAllPolicy(t, dir)
	env := map[string]string{"RELAY_DIR": dir}

	// Register two agents
	_, _, code := runRelay(t, bin, env, "register", "sender", "--agent", "sender")
	if code != 0 {
		t.Fatalf("register sender: exit %d", code)
	}
	_, _, code = runRelay(t, bin, env, "register", "receiver", "--agent", "receiver")
	if code != 0 {
		t.Fatalf("register receiver: exit %d", code)
	}

	// Send a message
	stdout, _, code := runRelay(t, bin, env, "send", "receiver", "hello from e2e", "--agent", "sender")
	if code != 0 {
		t.Fatalf("send: exit %d", code)
	}
	if !strings.Contains(strings.ToLower(stdout), "sent") {
		t.Errorf("send output should indicate success, got: %q", stdout)
	}

	// Read back as receiver
	stdout, _, code = runRelay(t, bin, env, "read", "--agent", "receiver", "--json")
	if code != 0 {
		t.Fatalf("read: exit %d", code)
	}
	if !strings.Contains(stdout, "hello from e2e") {
		t.Errorf("read output should contain message body, got: %q", stdout)
	}

	// Parse JSON to verify structure
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("read --json should return valid JSON array: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestE2E_SendToUnknownAgent(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "test-sender"}

	// Register sender but NOT recipient
	runRelay(t, bin, env, "register", "test-sender")

	// Send to unregistered agent — should fail gracefully
	_, stderr, code := runRelay(t, bin, env, "send", "nonexistent-agent", "should fail")
	if code == 0 {
		t.Fatal("expected non-zero exit for send to unknown agent")
	}
	if !strings.Contains(stderr, "not registered") && !strings.Contains(stderr, "send") {
		t.Errorf("stderr should mention the error, got: %q", stderr)
	}
}

func TestE2E_NoArgsExitsWithError(t *testing.T) {
	bin := buildRelay(t)

	_, _, code := runRelay(t, bin, nil)
	if code == 0 {
		t.Error("expected non-zero exit for no args")
	}
}

func TestE2E_UnknownCommandExitsWithError(t *testing.T) {
	bin := buildRelay(t)

	_, stderr, code := runRelay(t, bin, nil, "nonexistent-command")
	if code == 0 {
		t.Error("expected non-zero exit for unknown command")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr should mention unknown command, got: %q", stderr)
	}
}

func TestE2E_RegisterAndStatus(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "e2e-agent"}

	// Register
	_, _, code := runRelay(t, bin, env, "register", "e2e-agent", "--program", "test", "--task", "e2e testing")
	if code != 0 {
		t.Fatalf("register: exit %d", code)
	}

	// Status
	stdout, _, code := runRelay(t, bin, env, "status")
	if code != 0 {
		t.Fatalf("status: exit %d", code)
	}
	if !strings.Contains(stdout, "e2e-agent") {
		t.Errorf("status should list registered agent, got: %q", stdout)
	}
}

func TestE2E_Metrics(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	seedAllowAllPolicy(t, dir)
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "metrics-agent"}

	runRelay(t, bin, env, "register", "metrics-agent")
	runRelay(t, bin, env, "register", "other-agent", "--agent", "other-agent")
	runRelay(t, bin, env, "send", "other-agent", "test msg")

	// Text metrics
	stdout, _, code := runRelay(t, bin, env, "metrics")
	if code != 0 {
		t.Fatalf("metrics: exit %d", code)
	}
	if !strings.Contains(strings.ToUpper(stdout), "AGENTS") {
		t.Errorf("metrics output should contain AGENTS header, got: %q", stdout)
	}

	// JSON metrics
	stdout, _, code = runRelay(t, bin, env, "metrics", "--json")
	if code != 0 {
		t.Fatalf("metrics --json: exit %d", code)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &m); err != nil {
		t.Fatalf("metrics --json invalid: %v", err)
	}
	if _, ok := m["agents"]; !ok {
		t.Error("metrics JSON missing 'agents' key")
	}
}

func TestE2E_ReserveAndRelease(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	repo := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "res-agent"}

	runRelay(t, bin, env, "register", "res-agent")

	// Reserve
	_, _, code := runRelay(t, bin, env, "reserve", "src/**", "--repo", repo)
	if code != 0 {
		t.Fatalf("reserve: exit %d", code)
	}

	// Release
	_, _, code = runRelay(t, bin, env, "release", "src/**", "--repo", repo)
	if code != 0 {
		t.Fatalf("release: exit %d", code)
	}
}

func TestE2E_BroadcastMessage(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	seedAllowAllPolicy(t, dir)
	env := map[string]string{"RELAY_DIR": dir}

	// Register 3 agents
	runRelay(t, bin, env, "register", "broadcaster", "--agent", "broadcaster")
	runRelay(t, bin, env, "register", "listener-a", "--agent", "listener-a")
	runRelay(t, bin, env, "register", "listener-b", "--agent", "listener-b")

	// Broadcast
	stdout, _, code := runRelay(t, bin, env, "send", "--broadcast", "attention everyone", "--agent", "broadcaster")
	if code != 0 {
		t.Fatalf("broadcast: exit %d", code)
	}
	if !strings.Contains(stdout, "broadcast") {
		t.Errorf("broadcast output should confirm, got: %q", stdout)
	}

	// Both listeners should have the message
	for _, agent := range []string{"listener-a", "listener-b"} {
		stdout, _, code := runRelay(t, bin, env, "read", "--agent", agent, "--json")
		if code != 0 {
			t.Errorf("read %s: exit %d", agent, code)
		}
		if !strings.Contains(stdout, "attention everyone") {
			t.Errorf("%s should have broadcast message, got: %q", agent, stdout)
		}
	}
}

func TestE2E_GarbageCollection(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "gc-agent"}

	runRelay(t, bin, env, "register", "gc-agent")

	// Dry run
	stdout, _, code := runRelay(t, bin, env, "gc", "--dry-run")
	if code != 0 {
		t.Fatalf("gc --dry-run: exit %d", code)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("gc dry run output unexpected: %q", stdout)
	}

	// Actual GC
	_, _, code = runRelay(t, bin, env, "gc")
	if code != 0 {
		t.Fatalf("gc: exit %d", code)
	}
}

// TestE2E_FullAgentWorkflow exercises a realistic multi-agent coordination
// scenario through the compiled binary: register agents, reserve files,
// send typed messages, read with filters, check status JSON, and release.
func TestE2E_FullAgentWorkflow(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	repo := t.TempDir()
	seedAllowAllPolicy(t, dir)
	env := map[string]string{"RELAY_DIR": dir}

	// Register agents with different roles
	runRelay(t, bin, env, "register", "athena", "--agent", "athena", "--skills", "orchestration", "--task", "coordinate")
	runRelay(t, bin, env, "register", "worker-1", "--agent", "worker-1", "--skills", "go,testing", "--task", "br-42")

	// Worker reserves files
	_, _, code := runRelay(t, bin, env, "reserve", "src/auth/**", "--repo", repo, "--agent", "worker-1", "--reason", "br-42")
	if code != 0 {
		t.Fatalf("reserve: exit %d", code)
	}

	// Send typed message with payload
	_, _, code = runRelay(t, bin, env, "send", "athena", "auth refactor complete",
		"--agent", "worker-1",
		"--type", "task_result",
		"--payload", `{"exit_code":0,"files":["auth.go","auth_test.go"]}`,
		"--thread", "br-42",
		"--priority", "high")
	if code != 0 {
		t.Fatalf("send typed: exit %d", code)
	}

	// Read as athena with JSON and verify all fields survived
	stdout, _, code := runRelay(t, bin, env, "read", "--agent", "athena", "--json")
	if code != 0 {
		t.Fatalf("read: exit %d", code)
	}
	if !strings.Contains(stdout, "task_result") {
		t.Errorf("read output should contain message type, got: %q", stdout)
	}
	if !strings.Contains(stdout, "br-42") {
		t.Errorf("read output should contain thread, got: %q", stdout)
	}
	if !strings.Contains(stdout, "exit_code") {
		t.Errorf("read output should contain payload, got: %q", stdout)
	}

	// Parse to verify JSON structure
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &msgs); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}

	// Status JSON should show both agents
	stdout, _, code = runRelay(t, bin, env, "status", "--json", "--agent", "athena")
	if code != 0 {
		t.Fatalf("status: exit %d", code)
	}
	if !strings.Contains(stdout, "worker-1") {
		t.Errorf("status should list worker-1")
	}

	// Release and verify
	_, _, code = runRelay(t, bin, env, "release", "src/auth/**", "--repo", repo, "--agent", "worker-1")
	if code != 0 {
		t.Fatalf("release: exit %d", code)
	}
}

// TestE2E_HeartbeatAndCard exercises the heartbeat→card update path
// which is how agents maintain their presence in the system.
func TestE2E_HeartbeatAndCard(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "hb-agent"}

	runRelay(t, bin, env, "register", "hb-agent", "--skills", "go")

	// Heartbeat with task update
	_, _, code := runRelay(t, bin, env, "heartbeat", "--task", "br-99")
	if code != 0 {
		t.Fatalf("heartbeat --task: exit %d", code)
	}

	// Card should reflect working status
	stdout, _, code := runRelay(t, bin, env, "card", "--json")
	if code != 0 {
		t.Fatalf("card --json: exit %d", code)
	}
	if !strings.Contains(stdout, "working") {
		t.Errorf("card should show working status after heartbeat --task, got: %q", stdout)
	}
	if !strings.Contains(stdout, "br-99") {
		t.Errorf("card should show current task, got: %q", stdout)
	}

	// Heartbeat --idle should clear task
	_, _, code = runRelay(t, bin, env, "heartbeat", "--idle")
	if code != 0 {
		t.Fatalf("heartbeat --idle: exit %d", code)
	}

	stdout, _, code = runRelay(t, bin, env, "card", "--json")
	if code != 0 {
		t.Fatalf("card --json after idle: exit %d", code)
	}
	if !strings.Contains(stdout, "idle") {
		t.Errorf("card should show idle status after --idle, got: %q", stdout)
	}
}

// TestE2E_ReserveConflictAndForce exercises the reservation conflict
// detection and force-override path through the binary.
func TestE2E_ReserveConflictAndForce(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	repo := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir}

	runRelay(t, bin, env, "register", "agent-1", "--agent", "agent-1")
	runRelay(t, bin, env, "register", "agent-2", "--agent", "agent-2")

	// agent-1 reserves
	_, _, code := runRelay(t, bin, env, "reserve", "file.go", "--repo", repo, "--agent", "agent-1")
	if code != 0 {
		t.Fatalf("initial reserve: exit %d", code)
	}

	// agent-2 should get conflict
	_, stderr, code := runRelay(t, bin, env, "reserve", "file.go", "--repo", repo, "--agent", "agent-2")
	if code == 0 {
		t.Fatal("expected conflict, got exit 0")
	}
	if !strings.Contains(stderr, "conflict") {
		t.Errorf("stderr should mention conflict, got: %q", stderr)
	}

	// agent-2 with --force should succeed
	_, _, code = runRelay(t, bin, env, "reserve", "file.go", "--repo", repo, "--agent", "agent-2", "--force")
	if code != 0 {
		t.Fatalf("force reserve: exit %d", code)
	}
}

// TestE2E_CommandPostAndList exercises the command queue through the binary.
func TestE2E_CommandPostAndList(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "cmd-agent"}

	runRelay(t, bin, env, "register", "cmd-agent")

	// Post a command
	stdout, _, code := runRelay(t, bin, env, "cmd", "session:main", "/verify", "repo", "br-42", "--json")
	if code != 0 {
		t.Fatalf("cmd: exit %d", code)
	}
	if !strings.Contains(stdout, "/verify") {
		t.Errorf("cmd output should contain command, got: %q", stdout)
	}

	// Status should show pending command
	stdout, _, code = runRelay(t, bin, env, "status")
	if code != 0 {
		t.Fatalf("status: exit %d", code)
	}
	if !strings.Contains(stdout, "/verify") {
		t.Errorf("status should show pending command, got: %q", stdout)
	}
}

// TestE2E_DefaultAllowPolicyNoFile verifies that when no activation-policy.toml
// exists, agents can send messages to each other (default-allow).
func TestE2E_DefaultAllowPolicyNoFile(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir}

	// Register two agents — do NOT seed any policy file
	runRelay(t, bin, env, "register", "sender", "--agent", "sender")
	runRelay(t, bin, env, "register", "receiver", "--agent", "receiver")

	// Send should succeed without any policy file (default-allow)
	stdout, stderr, code := runRelay(t, bin, env, "send", "receiver", "no policy needed", "--agent", "sender")
	if code != 0 {
		t.Fatalf("send should succeed with default-allow (no policy file): exit %d, stderr: %q", code, stderr)
	}
	if !strings.Contains(strings.ToLower(stdout), "sent") {
		t.Errorf("send output should indicate success, got: %q", stdout)
	}

	// Read back to confirm delivery
	stdout, _, code = runRelay(t, bin, env, "read", "--agent", "receiver", "--json")
	if code != 0 {
		t.Fatalf("read: exit %d", code)
	}
	if !strings.Contains(stdout, "no policy needed") {
		t.Errorf("message should be delivered, got: %q", stdout)
	}
}

// TestE2E_DefaultAllowPolicyReset verifies that relay policy --reset
// produces a default-allow policy.
func TestE2E_DefaultAllowPolicyReset(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "policy-agent"}

	runRelay(t, bin, env, "register", "policy-agent")

	// Write a deny policy first
	policyPath := filepath.Join(dir, "activation-policy.toml")
	if err := os.WriteFile(policyPath, []byte("default = \"deny\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset should produce allow
	stdout, _, code := runRelay(t, bin, env, "policy", "--reset")
	if code != 0 {
		t.Fatalf("policy --reset: exit %d", code)
	}
	if !strings.Contains(stdout, "allow") {
		t.Errorf("reset output should mention allow, got: %q", stdout)
	}

	// Show should confirm allow
	stdout, _, code = runRelay(t, bin, env, "policy", "--show")
	if code != 0 {
		t.Fatalf("policy --show: exit %d", code)
	}
	if !strings.Contains(stdout, "allow") {
		t.Errorf("policy show should say allow after reset, got: %q", stdout)
	}
}

// TestE2E_RegisterTmuxSession verifies that --tmux-session is persisted
// to meta.json and survives a round-trip through the binary.
func TestE2E_RegisterTmuxSession(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	env := map[string]string{"RELAY_DIR": dir, "RELAY_AGENT": "tmux-agent"}

	_, _, code := runRelay(t, bin, env, "register", "tmux-agent", "--tmux-session", "polis-boss")
	if code != 0 {
		t.Fatalf("register: exit %d", code)
	}

	// Read meta.json directly
	data, err := os.ReadFile(filepath.Join(dir, "agents", "tmux-agent", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tmux_session"`) || !strings.Contains(string(data), "polis-boss") {
		t.Errorf("meta.json should contain tmux_session=polis-boss, got: %s", data)
	}
}

// TestE2E_SendWakeWithTmuxSessionLogsActivation verifies that relay send --wake
// with a tmux_session agent attempts the work send path and logs to the
// activation log, regardless of whether work is available.
func TestE2E_SendWakeWithTmuxSessionLogsActivation(t *testing.T) {
	bin := buildRelay(t)
	dir := t.TempDir()
	seedAllowAllPolicy(t, dir)
	env := map[string]string{"RELAY_DIR": dir}

	// Register target with tmux_session (no gateway_url)
	runRelay(t, bin, env, "register", "wake-target", "--agent", "wake-target", "--tmux-session", "test-sess")
	runRelay(t, bin, env, "register", "wake-sender", "--agent", "wake-sender")

	// Send with --wake — work send will fail (no real session), but should not crash
	_, _, code := runRelay(t, bin, env, "send", "wake-target", "check in please", "--wake", "--agent", "wake-sender")
	// Exit 0 expected — message delivered to inbox, wake failure is non-fatal
	if code != 0 {
		t.Fatalf("send --wake: exit %d (should be 0 even if wake injection fails)", code)
	}

	// Message should be in inbox regardless of wake outcome
	stdout, _, code := runRelay(t, bin, env, "read", "--agent", "wake-target", "--json")
	if code != 0 {
		t.Fatalf("read: exit %d", code)
	}
	if !strings.Contains(stdout, "check in please") {
		t.Errorf("message should be delivered to inbox, got: %q", stdout)
	}

	// Activation log should have an entry for this wake attempt
	logData, err := os.ReadFile(filepath.Join(dir, "activation-log.jsonl"))
	if err != nil {
		t.Fatalf("read activation log: %v", err)
	}
	logStr := string(logData)
	if !strings.Contains(logStr, "wake-target") {
		t.Errorf("activation log should mention target, got: %q", logStr)
	}
}
