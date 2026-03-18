package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/hermes-relay/internal/store"
)

func captureRunStderr(t *testing.T, args ...string) (int, string) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	full := append([]string{"relay"}, args...)
	code := Run(full)

	w.Close()
	os.Stderr = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data)
}

func TestPolicyShowEmpty(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()
	if err := os.Remove(filepath.Join(dir, "activation-policy.toml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	code, out := captureRun(t, "policy", "--show")
	if code != 0 {
		t.Fatalf("policy --show failed with code %d", code)
	}
	if !strings.Contains(out, "allow") {
		t.Errorf("expected default allow in output, got: %q", out)
	}
}

func TestPolicyShowJSON(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code, out := captureRun(t, "policy", "--show", "--json")
	if code != 0 {
		t.Fatalf("policy --show --json failed with code %d", code)
	}
	out = strings.TrimSpace(out)
	if !json.Valid([]byte(out)) {
		t.Errorf("policy --show --json should produce valid JSON, got: %q", out)
	}
}

func TestPolicyAllowAddsRule(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("policy", "--allow", "hermes", "iris")
	if code != 0 {
		t.Fatalf("policy --allow failed with code %d", code)
	}

	// Verify rule persisted
	s, _ := store.New(dir)
	p, err := s.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Allow) != 1 {
		t.Fatalf("expected 1 allow rule, got %d", len(p.Allow))
	}
	if p.Allow[0].From != "hermes" || p.Allow[0].To != "iris" {
		t.Errorf("unexpected allow rule: %+v", p.Allow[0])
	}
}

func TestPolicyDenyAddsRule(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	code := run("policy", "--deny", "chiron", "athena")
	if code != 0 {
		t.Fatalf("policy --deny failed with code %d", code)
	}

	s, _ := store.New(dir)
	p, err := s.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Deny) != 1 {
		t.Fatalf("expected 1 deny rule, got %d", len(p.Deny))
	}
	if p.Deny[0].From != "chiron" || p.Deny[0].To != "athena" {
		t.Errorf("unexpected deny rule: %+v", p.Deny[0])
	}
}

func TestPolicyReset(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Add some rules first
	run("policy", "--allow", "athena", "*")
	run("policy", "--deny", "chiron", "iris")

	// Reset
	code, out := captureRun(t, "policy", "--reset")
	if code != 0 {
		t.Fatalf("policy --reset failed with code %d", code)
	}
	if !strings.Contains(out, "default allow") {
		t.Fatalf("expected reset output to mention default allow, got: %q", out)
	}

	// Verify clean slate
	s, _ := store.New(dir)
	p, err := s.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Allow) != 0 {
		t.Errorf("expected 0 allow rules after reset, got %d", len(p.Allow))
	}
	if len(p.Deny) != 0 {
		t.Errorf("expected 0 deny rules after reset, got %d", len(p.Deny))
	}
	if p.Default != "allow" {
		t.Errorf("expected default allow after reset, got %s", p.Default)
	}
}

func TestPolicyNoFlags(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("policy")
	if code != 1 {
		t.Errorf("expected exit 1 for policy with no flags, got %d", code)
	}
}

func TestPolicyAllowMissingArgs(t *testing.T) {
	_, cleanup := setup(t)
	defer cleanup()

	code := run("policy", "--allow", "hermes")
	if code != 1 {
		t.Errorf("expected exit 1 for policy --allow with missing to, got %d", code)
	}
}

func TestSendWakeDeniedByPolicy(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	// Register target with gateway URL
	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	// Write an explicit default-deny policy (no rules allow test-agent)
	s, _ := store.New(dir)
	policy := &store.ActivationPolicy{Default: "deny"}
	if err := s.SavePolicy(policy); err != nil {
		t.Fatal(err)
	}

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code, stderr := captureRunStderr(t, "send", "target", "hello policy", "--wake")
	if code != 1 {
		t.Fatalf("send --wake should fail with code 1 when denied, got %d", code)
	}
	if !strings.Contains(stderr, "unauthorized by activation policy") {
		t.Fatalf("expected clear policy denial message, got: %q", stderr)
	}

	// Should NOT have called openclaw (policy denied)
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			t.Errorf("should not call openclaw when policy denies, got: %s", c)
		}
	}

	// Message should NOT be delivered to inbox
	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hello policy") {
		t.Error("message should not be delivered when send is denied by policy")
	}
}

func TestSendWakeAllowedByPolicy(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	// Write policy that allows test-agent to wake target
	s, _ := store.New(dir)
	policy := &store.ActivationPolicy{
		Default: "deny",
		Allow:   []store.PolicyRule{{From: "test-agent", To: "target"}},
	}
	if err := s.SavePolicy(policy); err != nil {
		t.Fatal(err)
	}

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "hello allowed", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	// Should have called openclaw (policy allows)
	foundOpenclaw := false
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			foundOpenclaw = true
			break
		}
	}
	if !foundOpenclaw {
		t.Errorf("expected openclaw call when policy allows, got: %v", calls)
	}
}

func TestSendWakeNoPolicyFileDefaultAllow(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")
	if err := os.Remove(filepath.Join(dir, "activation-policy.toml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	// No policy file — should default to allow
	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "no policy file", "--wake")
	if code != 0 {
		t.Fatalf("send --wake should succeed with default allow when no policy file, got %d", code)
	}

	// Should have called openclaw (default allow)
	foundOpenclaw := false
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			foundOpenclaw = true
			break
		}
	}
	if !foundOpenclaw {
		t.Errorf("expected openclaw call with default allow (no policy file), got: %v", calls)
	}
}

func TestSendDeniedByPolicyWithoutWake(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "target")
	run("register", "test-agent")

	s, _ := store.New(dir)
	policy := &store.ActivationPolicy{Default: "deny"}
	if err := s.SavePolicy(policy); err != nil {
		t.Fatal(err)
	}

	code, stderr := captureRunStderr(t, "send", "target", "plain send denied")
	if code != 1 {
		t.Fatalf("send should fail with code 1 when denied, got %d", code)
	}
	if !strings.Contains(stderr, "unauthorized by activation policy") {
		t.Fatalf("expected clear policy denial message, got: %q", stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agents", "target", "inbox.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plain send denied") {
		t.Error("message should not be written when send is denied by policy")
	}
}

func TestSendWakeWildcardPolicy(t *testing.T) {
	dir, cleanup := setup(t)
	defer cleanup()

	run("register", "target", "--gateway-url", "ws://localhost:4000/")
	run("register", "test-agent")

	// Wildcard allow for test-agent
	s, _ := store.New(dir)
	policy := &store.ActivationPolicy{
		Default: "deny",
		Allow:   []store.PolicyRule{{From: "test-agent", To: "*"}},
	}
	if err := s.SavePolicy(policy); err != nil {
		t.Fatal(err)
	}

	var calls []string
	withMockExec(t, func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("true")
	})

	code := run("send", "target", "wildcard test", "--wake")
	if code != 0 {
		t.Fatalf("send --wake failed with code %d", code)
	}

	foundOpenclaw := false
	for _, c := range calls {
		if strings.Contains(c, "openclaw system event") {
			foundOpenclaw = true
			break
		}
	}
	if !foundOpenclaw {
		t.Errorf("expected openclaw call with wildcard allow, got: %v", calls)
	}
}

// Ensure unused imports don't cause errors
var _ = time.Now
