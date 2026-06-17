package provider

import (
	"regexp"
	"testing"
	"time"
)

func TestBranchTimestampSuffix(t *testing.T) {
	// 2026-06-16T13:03:00.002Z -> 20260616130300002
	ts := time.Date(2026, time.June, 16, 13, 3, 0, 2*int(time.Millisecond), time.UTC)
	got := branchTimestampSuffix(ts)
	want := "20260616130300002"
	if got != want {
		t.Fatalf("branchTimestampSuffix() = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^\d{17}$`).MatchString(got) {
		t.Fatalf("branchTimestampSuffix() = %q, want 17 digits (YYYYMMDDHHMMSSmmm)", got)
	}
}

func TestResolveBranchTimestamp(t *testing.T) {
	now := time.Date(2026, time.June, 16, 13, 3, 0, 2*int(time.Millisecond), time.UTC)

	if got := resolveBranch("cus-tofu-git-provider", true, now); got != "cus-tofu-git-provider-20260616130300002" {
		t.Fatalf("resolveBranch with timestamp = %q", got)
	}
	if got := resolveBranch("cus-tofu-git-provider", false, now); got != "cus-tofu-git-provider" {
		t.Fatalf("resolveBranch without timestamp = %q", got)
	}
	if got := resolveBranch("", true, now); got != "" {
		t.Fatalf("resolveBranch with empty branch = %q, want empty", got)
	}
}

// TestResolveConfiguredBranch verifies that a caller-supplied suffix produces a
// stable "<branch>-<suffix>" name, takes precedence over append_timestamp, and
// that the legacy behaviour is preserved when no suffix is given.
func TestResolveConfiguredBranch(t *testing.T) {
	now := time.Date(2026, time.June, 16, 13, 3, 0, 2*int(time.Millisecond), time.UTC)

	if got := resolveConfiguredBranch("hsb-tofu-git-provider", "20260617120000", false, now); got != "hsb-tofu-git-provider-20260617120000" {
		t.Fatalf("with suffix = %q", got)
	}
	if got := resolveConfiguredBranch("b", "run42", true, now); got != "b-run42" {
		t.Fatalf("suffix should take precedence over append_timestamp, got %q", got)
	}
	if got := resolveConfiguredBranch("b", "", true, now); got != "b-20260616130300002" {
		t.Fatalf("legacy timestamp branch = %q", got)
	}
	if got := resolveConfiguredBranch("b", "", false, now); got != "b" {
		t.Fatalf("plain branch = %q", got)
	}
	if got := resolveConfiguredBranch("", "x", false, now); got != "" {
		t.Fatalf("empty branch should stay empty, got %q", got)
	}
}

func TestResolveConfiguredBranchStableAcrossPhases(t *testing.T) {
	// A fixed, caller-supplied suffix must yield the same branch name even when
	// the wall clock differs between phases (unlike append_timestamp_to_branch).
	t1 := time.Date(2026, time.June, 16, 13, 3, 0, 1*int(time.Millisecond), time.UTC)
	t2 := time.Date(2026, time.June, 16, 14, 5, 0, 9*int(time.Millisecond), time.UTC)
	if resolveConfiguredBranch("b", "fixed", false, t1) != resolveConfiguredBranch("b", "fixed", false, t2) {
		t.Fatalf("expected identical branch names across phases with a fixed suffix")
	}
}

func TestResolveBranchUniquePerRun(t *testing.T) {
	t1 := time.Date(2026, time.June, 16, 13, 3, 0, 1*int(time.Millisecond), time.UTC)
	t2 := time.Date(2026, time.June, 16, 13, 3, 0, 2*int(time.Millisecond), time.UTC)
	if resolveBranch("b", true, t1) == resolveBranch("b", true, t2) {
		t.Fatalf("expected distinct branch names for runs that differ by milliseconds")
	}
}
