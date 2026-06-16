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

func TestResolveBranch(t *testing.T) {
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

func TestResolveBranchUniquePerRun(t *testing.T) {
	t1 := time.Date(2026, time.June, 16, 13, 3, 0, 1*int(time.Millisecond), time.UTC)
	t2 := time.Date(2026, time.June, 16, 13, 3, 0, 2*int(time.Millisecond), time.UTC)
	if resolveBranch("b", true, t1) == resolveBranch("b", true, t2) {
		t.Fatalf("expected distinct branch names for runs that differ by milliseconds")
	}
}
