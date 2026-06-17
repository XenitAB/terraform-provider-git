package provider

import "testing"

// TestResolveBranch verifies the branch resolution precedence: an explicit
// override wins, then the provider-configured branch, then the "main" default.
func TestResolveBranch(t *testing.T) {
	testCases := []struct {
		name           string
		providerBranch string
		override       string
		expected       string
	}{
		{
			name:           "override takes precedence over provider branch",
			providerBranch: "develop",
			override:       "feature-123",
			expected:       "feature-123",
		},
		{
			name:           "falls back to provider branch when override empty",
			providerBranch: "develop",
			override:       "",
			expected:       "develop",
		},
		{
			name:           "falls back to main when override and provider branch empty",
			providerBranch: "",
			override:       "",
			expected:       "main",
		},
		{
			name:           "override used even when provider branch empty",
			providerBranch: "",
			override:       "release",
			expected:       "release",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prd := &ProviderResourceData{branch: tc.providerBranch}
			if actual := prd.ResolveBranch(tc.override); actual != tc.expected {
				t.Errorf("ResolveBranch(%q) with provider branch %q = %q, want %q", tc.override, tc.providerBranch, actual, tc.expected)
			}
		})
	}
}
