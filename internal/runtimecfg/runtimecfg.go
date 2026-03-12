package runtimecfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveDir applies the relay directory precedence:
// explicit argument -> RELAY_DIR -> ~/.relay.
func ResolveDir(explicit string) (string, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("RELAY_DIR"))
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".relay")
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	return filepath.Clean(dir), nil
}

// ResolveAgent applies the relay identity precedence:
// explicit argument -> RELAY_AGENT -> hostname.
func ResolveAgent(explicit string) (string, error) {
	agent := strings.TrimSpace(explicit)
	if agent != "" {
		return agent, nil
	}
	agent = strings.TrimSpace(os.Getenv("RELAY_AGENT"))
	if agent != "" {
		return agent, nil
	}
	agent, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("resolve agent name: %w", err)
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", fmt.Errorf("resolve agent name: empty hostname")
	}
	return agent, nil
}
