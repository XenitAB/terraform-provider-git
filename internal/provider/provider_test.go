package provider

import (
	"testing"
	"time"
)

func TestConfiguredBranchName(t *testing.T) {
	t.Parallel()

	now := func() time.Time {
		return time.Date(2026, time.June, 10, 14, 52, 59, 0, time.UTC)
	}

	testCases := map[string]struct {
		branch          string
		appendTimestamp bool
		expected        string
	}{
		"disabled": {
			branch:          "unbox-tofu-git-provider",
			appendTimestamp: false,
			expected:        "unbox-tofu-git-provider",
		},
		"enabled": {
			branch:          "unbox-tofu-git-provider",
			appendTimestamp: true,
			expected:        "unbox-tofu-git-provider-20260610145259",
		},
		"empty branch": {
			appendTimestamp: true,
			expected:        "",
		},
	}

	for name, testCase := range testCases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if actual := configuredBranchName(testCase.branch, testCase.appendTimestamp, now); actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}
