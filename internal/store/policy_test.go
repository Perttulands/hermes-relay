package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAllowedDefaultDenyNoRules(t *testing.T) {
	p := DefaultPolicy()
	if p.IsAllowed("hermes", "iris") {
		t.Error("default deny should block when no rules match")
	}
}

func TestIsAllowedDefaultAllow(t *testing.T) {
	p := &ActivationPolicy{Default: "allow"}
	if !p.IsAllowed("hermes", "iris") {
		t.Error("default allow should permit when no rules match")
	}
}

func TestIsAllowedWildcardTo(t *testing.T) {
	p := &ActivationPolicy{
		Default: "deny",
		Allow:   []PolicyRule{{From: "athena", To: "*"}},
	}
	if !p.IsAllowed("athena", "hermes") {
		t.Error("athena should be able to wake hermes via wildcard to")
	}
	if !p.IsAllowed("athena", "iris") {
		t.Error("athena should be able to wake iris via wildcard to")
	}
	if p.IsAllowed("hermes", "iris") {
		t.Error("hermes should not be able to wake iris")
	}
}

func TestIsAllowedWildcardFrom(t *testing.T) {
	p := &ActivationPolicy{
		Default: "deny",
		Allow:   []PolicyRule{{From: "*", To: "athena"}},
	}
	if !p.IsAllowed("hermes", "athena") {
		t.Error("anyone should be able to wake athena via wildcard from")
	}
	if p.IsAllowed("hermes", "iris") {
		t.Error("hermes should not be able to wake iris")
	}
}

func TestIsAllowedSpecificRule(t *testing.T) {
	p := &ActivationPolicy{
		Default: "deny",
		Allow:   []PolicyRule{{From: "hermes", To: "iris"}},
	}
	if !p.IsAllowed("hermes", "iris") {
		t.Error("hermes should be able to wake iris")
	}
	if p.IsAllowed("hermes", "athena") {
		t.Error("hermes should not be able to wake athena")
	}
	if p.IsAllowed("iris", "hermes") {
		t.Error("iris should not be able to wake hermes")
	}
}

func TestDenyOverridesAllow(t *testing.T) {
	p := &ActivationPolicy{
		Default: "deny",
		Allow:   []PolicyRule{{From: "athena", To: "*"}},
		Deny:    []PolicyRule{{From: "athena", To: "chiron"}},
	}
	if !p.IsAllowed("athena", "hermes") {
		t.Error("athena should be able to wake hermes")
	}
	if p.IsAllowed("athena", "chiron") {
		t.Error("deny rule should override allow for athena→chiron")
	}
}

func TestDenyOverridesDefaultAllow(t *testing.T) {
	p := &ActivationPolicy{
		Default: "allow",
		Deny:    []PolicyRule{{From: "chiron", To: "athena"}},
	}
	if p.IsAllowed("chiron", "athena") {
		t.Error("deny should block chiron→athena even with default allow")
	}
	if !p.IsAllowed("hermes", "athena") {
		t.Error("hermes→athena should be allowed under default allow")
	}
}

func TestParsePolicyTOML(t *testing.T) {
	input := `
# Activation policy
default = "deny"

[[allow]]
from = "athena"
to = "*"

[[allow]]
from = "hermes"
to = "iris"

[[deny]]
from = "athena"
to = "chiron"
`
	p, err := parsePolicyTOML(input)
	if err != nil {
		t.Fatal(err)
	}
	if p.Default != "deny" {
		t.Errorf("expected default=deny, got %s", p.Default)
	}
	if len(p.Allow) != 2 {
		t.Fatalf("expected 2 allow rules, got %d", len(p.Allow))
	}
	if p.Allow[0].From != "athena" || p.Allow[0].To != "*" {
		t.Errorf("unexpected allow[0]: %+v", p.Allow[0])
	}
	if p.Allow[1].From != "hermes" || p.Allow[1].To != "iris" {
		t.Errorf("unexpected allow[1]: %+v", p.Allow[1])
	}
	if len(p.Deny) != 1 {
		t.Fatalf("expected 1 deny rule, got %d", len(p.Deny))
	}
	if p.Deny[0].From != "athena" || p.Deny[0].To != "chiron" {
		t.Errorf("unexpected deny[0]: %+v", p.Deny[0])
	}
}

func TestParsePolicyTOMLInvalidDefault(t *testing.T) {
	input := `default = "banana"`
	_, err := parsePolicyTOML(input)
	if err == nil {
		t.Error("expected error for invalid default")
	}
}

func TestParsePolicyTOMLTrustLevel(t *testing.T) {
	input := `
default = "deny"

[[allow]]
from = "codex"
to = "athena"
trust_level = 1

[[deny]]
from = "codex"
to = "inside"
trust_level = 2
`
	p, err := parsePolicyTOML(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Allow) != 1 || p.Allow[0].TrustLevel != 1 {
		t.Fatalf("expected allow trust_level=1, got %+v", p.Allow)
	}
	if len(p.Deny) != 1 || p.Deny[0].TrustLevel != 2 {
		t.Fatalf("expected deny trust_level=2, got %+v", p.Deny)
	}
}

func TestLoadPolicyMissingFile(t *testing.T) {
	p, err := LoadPolicy("/nonexistent/activation-policy.toml")
	if err != nil {
		t.Fatal(err)
	}
	if p.Default != "deny" {
		t.Errorf("missing file should return default deny, got %s", p.Default)
	}
}

func TestSaveAndLoadPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activation-policy.toml")

	original := &ActivationPolicy{
		Default: "deny",
		Allow: []PolicyRule{
			{From: "athena", To: "*"},
			{From: "hermes", To: "iris"},
		},
		Deny: []PolicyRule{
			{From: "chiron", To: "athena"},
		},
	}

	if err := SavePolicy(path, original); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatal("policy file should exist after save")
	}

	loaded, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Default != original.Default {
		t.Errorf("default mismatch: got %s, want %s", loaded.Default, original.Default)
	}
	if len(loaded.Allow) != len(original.Allow) {
		t.Fatalf("allow count mismatch: got %d, want %d", len(loaded.Allow), len(original.Allow))
	}
	for i, r := range loaded.Allow {
		if r.From != original.Allow[i].From || r.To != original.Allow[i].To {
			t.Errorf("allow[%d] mismatch: got %+v, want %+v", i, r, original.Allow[i])
		}
	}
	if len(loaded.Deny) != len(original.Deny) {
		t.Fatalf("deny count mismatch: got %d, want %d", len(loaded.Deny), len(original.Deny))
	}
	for i, r := range loaded.Deny {
		if r.From != original.Deny[i].From || r.To != original.Deny[i].To {
			t.Errorf("deny[%d] mismatch: got %+v, want %+v", i, r, original.Deny[i])
		}
	}
}

func TestDirLoadPolicy(t *testing.T) {
	dir := t.TempDir()
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// No file: should return default deny
	p, err := d.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Default != "deny" {
		t.Errorf("expected default deny, got %s", p.Default)
	}

	// Save and reload
	p.Allow = append(p.Allow, PolicyRule{From: "athena", To: "*"})
	if err := d.SavePolicy(p); err != nil {
		t.Fatal(err)
	}

	p2, err := d.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Allow) != 1 {
		t.Errorf("expected 1 allow rule after save/load, got %d", len(p2.Allow))
	}
}

func TestTrustLevelForAgent(t *testing.T) {
	p := &ActivationPolicy{
		Default: "deny",
		Allow: []PolicyRule{
			{From: "codex", To: "athena", TrustLevel: 1},
			{From: "codex", To: "hestia", TrustLevel: 2},
		},
		Deny: []PolicyRule{
			{From: "codex", To: "inside", TrustLevel: 3},
		},
	}
	if got := p.TrustLevelForAgent("codex"); got != 3 {
		t.Fatalf("expected trust level 3, got %d", got)
	}
	if got := p.TrustLevelForAgent("unknown"); got != 0 {
		t.Fatalf("expected trust level 0 for unknown agent, got %d", got)
	}
}

func TestDirLoadPolicyMergesGraduationTrust(t *testing.T) {
	dir := t.TempDir()
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	policy := &ActivationPolicy{
		Default: "deny",
		Allow: []PolicyRule{
			{From: "codex", To: "athena"},
		},
	}
	if err := d.SavePolicy(policy); err != nil {
		t.Fatal(err)
	}

	graduation := `
[[agent]]
name = "codex"
trust_level = 1

[[agent]]
name = "outside"
trust_level = 2
`
	if err := os.WriteFile(filepath.Join(dir, "graduation.toml"), []byte(graduation), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := d.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.IsAllowed("codex", "athena") {
		t.Fatal("expected allow rule codex -> athena to remain active")
	}
	if loaded.IsAllowed("outside", "athena") {
		t.Fatal("graduation must not grant allow/deny permissions")
	}
	if got := loaded.TrustLevelForAgent("codex"); got != 1 {
		t.Fatalf("expected codex trust level 1 from graduation, got %d", got)
	}
	if got := loaded.TrustLevelForAgent("outside"); got != 2 {
		t.Fatalf("expected outside trust level 2 from graduation, got %d", got)
	}
}

func TestLoadGraduationTrustLevelsRevokedIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graduation.toml")
	content := `
[[agent]]
name = "codex"
trust_level = 1

[[agent]]
name = "codex"
trust_level = 4
revoked = true
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	trust, err := loadGraduationTrustLevels(path)
	if err != nil {
		t.Fatal(err)
	}
	if trust["codex"] != 1 {
		t.Fatalf("expected revoked entries ignored and trust level 1 retained, got %d", trust["codex"])
	}
}
