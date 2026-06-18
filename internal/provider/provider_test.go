package provider

import (
	"testing"
)

// TestResolveConfiguredBranch verifies that a caller-supplied suffix produces a
// stable "<branch>-<suffix>" name and that the branch name is used as-is when no
// suffix is given.
func TestResolveConfiguredBranch(t *testing.T) {
	if got := resolveConfiguredBranch("hsb-tofu-git-provider", "20260617120000"); got != "hsb-tofu-git-provider-20260617120000" {
		t.Fatalf("with suffix = %q", got)
	}
	if got := resolveConfiguredBranch("b", "run42"); got != "b-run42" {
		t.Fatalf("with suffix = %q", got)
	}
	if got := resolveConfiguredBranch("b", ""); got != "b" {
		t.Fatalf("plain branch = %q", got)
	}
	if got := resolveConfiguredBranch("", "x"); got != "" {
		t.Fatalf("empty branch should stay empty, got %q", got)
	}
}

func TestResolveConfiguredBranchStableAcrossPhases(t *testing.T) {
	// A fixed, caller-supplied suffix must always yield the same branch name,
	// independent of when it is resolved.
	if resolveConfiguredBranch("b", "fixed") != resolveConfiguredBranch("b", "fixed") {
		t.Fatalf("expected identical branch names with a fixed suffix")
	}
}
