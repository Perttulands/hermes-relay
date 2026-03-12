package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDirExplicitWins(t *testing.T) {
	t.Setenv("RELAY_DIR", "/tmp/env-relay")
	dir, err := ResolveDir("/tmp/explicit-relay")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/explicit-relay" {
		t.Fatalf("ResolveDir explicit = %q, want %q", dir, "/tmp/explicit-relay")
	}
}

func TestResolveDirUsesEnv(t *testing.T) {
	t.Setenv("RELAY_DIR", "/tmp/env-relay")
	dir, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/env-relay" {
		t.Fatalf("ResolveDir env = %q, want %q", dir, "/tmp/env-relay")
	}
}

func TestResolveDirExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := ResolveDir("~/my-relay")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "my-relay")
	if dir != want {
		t.Fatalf("ResolveDir tilde = %q, want %q", dir, want)
	}
}

func TestResolveAgentExplicitWins(t *testing.T) {
	t.Setenv("RELAY_AGENT", "env-agent")
	agent, err := ResolveAgent(" explicit-agent ")
	if err != nil {
		t.Fatal(err)
	}
	if agent != "explicit-agent" {
		t.Fatalf("ResolveAgent explicit = %q, want %q", agent, "explicit-agent")
	}
}

func TestResolveAgentUsesEnv(t *testing.T) {
	t.Setenv("RELAY_AGENT", " env-agent ")
	agent, err := ResolveAgent("")
	if err != nil {
		t.Fatal(err)
	}
	if agent != "env-agent" {
		t.Fatalf("ResolveAgent env = %q, want %q", agent, "env-agent")
	}
}

func TestResolveAgentFallsBackToHostname(t *testing.T) {
	t.Setenv("RELAY_AGENT", "")
	agent, err := ResolveAgent("")
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if agent != host {
		t.Fatalf("ResolveAgent hostname = %q, want %q", agent, host)
	}
}
