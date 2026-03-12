package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ActivationPolicy controls which agents are allowed to wake other agents.
type ActivationPolicy struct {
	Default    string         `json:"default"` // "allow" or "deny"
	Allow      []PolicyRule   `json:"allow,omitempty"`
	Deny       []PolicyRule   `json:"deny,omitempty"`
	Graduation map[string]int `json:"-"`
}

// PolicyRule is a from→to permission (or denial) entry.
type PolicyRule struct {
	From       string `json:"from"`                 // agent name or "*"
	To         string `json:"to"`                   // agent name or "*"
	TrustLevel int    `json:"trust_level,omitempty"` // 0-4, defaults to 0
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

// TrustLevelForAgent returns the configured trust level for an agent.
// If no policy rule includes the agent, it returns 0 (outside/unknown).
func (p *ActivationPolicy) TrustLevelForAgent(agent string) int {
	highest := 0
	if p.Graduation != nil {
		if level := p.Graduation[agent]; level > highest {
			highest = level
		}
	}
	for _, r := range p.Allow {
		if r.From == agent && r.TrustLevel > highest {
			highest = r.TrustLevel
		}
	}
	for _, r := range p.Deny {
		if r.From == agent && r.TrustLevel > highest {
			highest = r.TrustLevel
		}
	}
	return highest
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

func (d *Dir) graduationPath() string {
	return filepath.Join(d.Root, "graduation.toml")
}

// LoadPolicy reads the activation policy from activation-policy.toml.
// Returns a default-deny policy if the file does not exist.
func (d *Dir) LoadPolicy() (*ActivationPolicy, error) {
	p, err := LoadPolicy(d.policyPath())
	if err != nil {
		return nil, err
	}
	trust, err := loadGraduationTrustLevels(d.graduationPath())
	if err != nil {
		return nil, err
	}
	p.Graduation = trust
	return p, nil
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
		if r.TrustLevel > 0 {
			sb.WriteString(fmt.Sprintf("trust_level = %d\n", r.TrustLevel))
		}
	}
	for _, r := range p.Deny {
		sb.WriteString("\n[[deny]]\n")
		sb.WriteString(fmt.Sprintf("from = %q\n", r.From))
		sb.WriteString(fmt.Sprintf("to = %q\n", r.To))
		if r.TrustLevel > 0 {
			sb.WriteString(fmt.Sprintf("trust_level = %d\n", r.TrustLevel))
		}
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
	currentTrustLevel := 0
	flushRule := func() {
		if currentFrom == "" && currentTo == "" && currentTrustLevel == 0 {
			return
		}
		rule := PolicyRule{From: currentFrom, To: currentTo, TrustLevel: currentTrustLevel}
		switch currentSection {
		case sectionAllow:
			p.Allow = append(p.Allow, rule)
		case sectionDeny:
			p.Deny = append(p.Deny, rule)
		}
		currentFrom, currentTo, currentTrustLevel = "", "", 0
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
		case "trust_level":
			level, err := strconv.Atoi(val)
			if err == nil && level >= 0 {
				currentTrustLevel = level
			}
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

func loadGraduationTrustLevels(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{}, nil
		}
		return nil, fmt.Errorf("read graduation: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	trust := make(map[string]int)
	inAgentSection := false
	currentName := ""
	currentTrust := 0
	currentRevoked := false

	flushEntry := func() {
		if !inAgentSection || currentName == "" || currentRevoked {
			return
		}
		if currentTrust > trust[currentName] {
			trust[currentName] = currentTrust
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[agent]]" {
			flushEntry()
			inAgentSection = true
			currentName = ""
			currentTrust = 0
			currentRevoked = false
			continue
		}
		if !inAgentSection {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = unquoteTOML(val)

		switch key {
		case "name":
			currentName = val
		case "trust_level":
			level, convErr := strconv.Atoi(val)
			if convErr == nil && level >= 0 {
				currentTrust = level
			}
		case "revoked":
			currentRevoked = strings.EqualFold(val, "true")
		}
	}
	flushEntry()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan graduation: %w", err)
	}
	return trust, nil
}
