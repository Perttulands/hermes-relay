package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ActivationPolicy controls which agents are allowed to wake other agents.
type ActivationPolicy struct {
	Default string       `json:"default"` // "allow" or "deny"
	Allow   []PolicyRule `json:"allow,omitempty"`
	Deny    []PolicyRule `json:"deny,omitempty"`
}

// PolicyRule is a from→to permission (or denial) entry.
type PolicyRule struct {
	From string `json:"from"` // agent name or "*"
	To   string `json:"to"`   // agent name or "*"
}

// IsAllowed checks whether agent `from` is allowed to wake agent `to`.
// Deny rules take precedence over allow rules.
// If default is "allow", everything is allowed unless explicitly denied.
// If default is "deny", everything is denied unless explicitly allowed.
func (p *ActivationPolicy) IsAllowed(from, to string) bool {
	// Check deny rules first (deny takes precedence)
	for _, r := range p.Deny {
		if matchesRule(r, from, to) {
			return false
		}
	}

	// Check allow rules
	for _, r := range p.Allow {
		if matchesRule(r, from, to) {
			return true
		}
	}

	// Fall back to default
	return p.Default == "allow"
}

func matchesRule(r PolicyRule, from, to string) bool {
	return (r.From == "*" || r.From == from) &&
		(r.To == "*" || r.To == to)
}

// DefaultPolicy returns a conservative default-deny policy.
func DefaultPolicy() *ActivationPolicy {
	return &ActivationPolicy{Default: "deny"}
}

// policyPath returns the path to activation-policy.toml.
func (d *Dir) policyPath() string {
	return filepath.Join(d.Root, "activation-policy.toml")
}

// LoadPolicy reads the activation policy from activation-policy.toml.
// Returns a default-deny policy if the file does not exist.
func (d *Dir) LoadPolicy() (*ActivationPolicy, error) {
	return LoadPolicy(d.policyPath())
}

// SavePolicy writes the activation policy to activation-policy.toml.
func (d *Dir) SavePolicy(p *ActivationPolicy) error {
	return SavePolicy(d.policyPath(), p)
}

// LoadPolicy reads an ActivationPolicy from a TOML file.
// Returns a default-deny policy if the file does not exist.
func LoadPolicy(path string) (*ActivationPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return nil, fmt.Errorf("read policy: %w", err)
	}
	return parsePolicyTOML(string(data))
}

// SavePolicy writes an ActivationPolicy as TOML.
func SavePolicy(path string, p *ActivationPolicy) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("default = %q\n", p.Default))

	for _, r := range p.Allow {
		sb.WriteString("\n[[allow]]\n")
		sb.WriteString(fmt.Sprintf("from = %q\n", r.From))
		sb.WriteString(fmt.Sprintf("to = %q\n", r.To))
	}
	for _, r := range p.Deny {
		sb.WriteString("\n[[deny]]\n")
		sb.WriteString(fmt.Sprintf("from = %q\n", r.From))
		sb.WriteString(fmt.Sprintf("to = %q\n", r.To))
	}

	return atomicWrite(path, []byte(sb.String()))
}

// parsePolicyTOML parses the minimal TOML subset used by activation-policy.toml.
// Supports: top-level `default = "..."`, `[[allow]]` and `[[deny]]` table arrays
// with `from` and `to` string fields.
func parsePolicyTOML(input string) (*ActivationPolicy, error) {
	p := &ActivationPolicy{Default: "deny"}
	scanner := bufio.NewScanner(strings.NewReader(input))

	type section int
	const (
		sectionNone section = iota
		sectionAllow
		sectionDeny
	)
	currentSection := sectionNone

	var currentFrom, currentTo string
	flushRule := func() {
		if currentFrom == "" && currentTo == "" {
			return
		}
		rule := PolicyRule{From: currentFrom, To: currentTo}
		switch currentSection {
		case sectionAllow:
			p.Allow = append(p.Allow, rule)
		case sectionDeny:
			p.Deny = append(p.Deny, rule)
		}
		currentFrom, currentTo = "", ""
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Table array headers
		if line == "[[allow]]" {
			flushRule()
			currentSection = sectionAllow
			continue
		}
		if line == "[[deny]]" {
			flushRule()
			currentSection = sectionDeny
			continue
		}

		// Key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = unquoteTOML(val)

		switch key {
		case "default":
			if currentSection == sectionNone {
				p.Default = val
			}
		case "from":
			currentFrom = val
		case "to":
			currentTo = val
		}
	}
	flushRule()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan policy: %w", err)
	}

	// Validate default
	if p.Default != "allow" && p.Default != "deny" {
		return nil, fmt.Errorf("invalid default policy %q: must be \"allow\" or \"deny\"", p.Default)
	}

	return p, nil
}

// unquoteTOML removes surrounding double quotes from a TOML string value.
func unquoteTOML(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
