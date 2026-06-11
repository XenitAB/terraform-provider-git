package provider

import (
"errors"
"testing"
)

func TestIsBranchNotFoundErr(t *testing.T) {
t.Parallel()

tests := map[string]struct {
err      error
expected bool
}{
"branch not found": {
err:      errors.New("unable to clone 'https://github.com/example/repo.git': couldn't find remote ref 'refs/heads/my-branch-20260611070810': git repository: 'https://github.com/example/repo.git'"),
expected: true,
},
"repo not found": {
err:      errors.New("unable to clone: repository not found: git repository: 'https://github.com/example/nonexistent.git'"),
expected: false,
},
"auth error": {
err:      errors.New("authentication required"),
expected: false,
},
"network error": {
err:      errors.New("dial tcp: connection refused"),
expected: false,
},
"exact phrase match": {
err:      errors.New("couldn't find remote ref 'refs/heads/feature'"),
expected: true,
},
}

for name, tc := range tests {
tc := tc
t.Run(name, func(t *testing.T) {
t.Parallel()
got := isBranchNotFoundErr(tc.err)
if got != tc.expected {
t.Errorf("isBranchNotFoundErr(%q) = %v, want %v", tc.err.Error(), got, tc.expected)
}
})
}
}
