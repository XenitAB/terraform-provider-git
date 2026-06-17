package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/fluxcd/flux2/pkg/manifestgen/sourcesecret"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
	fluxknownhosts "github.com/fluxcd/pkg/ssh/knownhosts"
	gogitv5 "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gogittransport "github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

type ProviderResourceData struct {
	url              string
	branch           string
	base_branch      string
	append_timestamp bool
	ssh              *Ssh
	http             *Http
	commits          *Commits
	ignore_updates   bool
}

func (prd *ProviderResourceData) IgnoreUpdates(ctx context.Context) bool {
	return prd.ignore_updates
}

func (prd *ProviderResourceData) Branch(ctx context.Context) string {
	return prd.branch
}

func (prd *ProviderResourceData) Commits(ctx context.Context) *Commits {
	return prd.commits
}

func (prd *ProviderResourceData) GetGitClient(ctx context.Context) (*gogit.Client, error) {
	return prd.GetGitClientForBranch(ctx, prd.ResolveBranch(""))
}

// ResolveBranch determines the effective branch to operate on. When override is
// non-empty it takes precedence, otherwise the provider-configured branch is
// used, falling back to "main" when neither is set.
func (prd *ProviderResourceData) ResolveBranch(override string) string {
	branch := override
	if branch == "" {
		branch = prd.branch
	}
	if branch == "" {
		branch = "main"
	}
	return branch
}

func (prd *ProviderResourceData) GetGitClientForBranch(ctx context.Context, branch string) (*gogit.Client, error) {
	client, tmpDir, err := prd.getGitClientForExistingBranch(ctx, branch)
	if err == nil {
		return client, nil
	}
	if !isMissingRemoteRefError(err) {
		return nil, err
	}

	os.RemoveAll(tmpDir)

	// Build fallback branch list: first try the detected default branch via
	// ls-remote, then fall back to the common defaults.
	fallbackBranches := []string{}
	seen := map[string]bool{branch: true}

	detectedBranch, lsRemoteErr := prd.detectDefaultBranch()
	if lsRemoteErr == nil && detectedBranch != "" && !seen[detectedBranch] {
		fallbackBranches = append(fallbackBranches, detectedBranch)
		seen[detectedBranch] = true
	}
	for _, b := range []string{"main", "master"} {
		if !seen[b] {
			fallbackBranches = append(fallbackBranches, b)
			seen[b] = true
		}
	}

	var fallbackErrors []string
	for _, fallbackBranch := range fallbackBranches {
		fallbackClient, fallbackTmpDir, fallbackErr := prd.getGitClientForExistingBranch(ctx, fallbackBranch)
		if fallbackErr != nil {
			os.RemoveAll(fallbackTmpDir)
			fallbackErrors = append(fallbackErrors, fmt.Sprintf("branch %q: %s", fallbackBranch, fallbackErr.Error()))
			continue
		}

		if switchErr := fallbackClient.SwitchBranch(ctx, branch); switchErr != nil {
			os.RemoveAll(fallbackTmpDir)
			return nil, fmt.Errorf("failed to switch to branch %q from fallback %q: %w", branch, fallbackBranch, switchErr)
		}

		refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
		if pushErr := fallbackClient.Push(ctx, repository.PushConfig{Refspecs: []string{refspec}}); pushErr != nil {
			os.RemoveAll(fallbackTmpDir)
			return nil, fmt.Errorf("failed to push branch %q from fallback %q: %w", branch, fallbackBranch, pushErr)
		}

		return fallbackClient, nil
	}

	// All fallbacks failed — build a descriptive error.
	var sb strings.Builder
	fmt.Fprintf(&sb, "branch %q does not exist on remote", branch)
	if lsRemoteErr != nil {
		fmt.Fprintf(&sb, "; fallback: could not detect default branch via ls-remote: %s", lsRemoteErr.Error())
	}
	if len(fallbackErrors) > 0 {
		fmt.Fprintf(&sb, "; tried fallbacks: %s", strings.Join(fallbackErrors, "; "))
	}
	return nil, fmt.Errorf("%s", sb.String())
}

func (prd *ProviderResourceData) GetGitClientForExistingBranch(ctx context.Context, branch string) (*gogit.Client, error) {
	client, _, err := prd.getGitClientForExistingBranch(ctx, branch)
	return client, err
}

func (prd *ProviderResourceData) getGitClientForExistingBranch(ctx context.Context, branch string) (*gogit.Client, string, error) {
	u, err := url.Parse(prd.url)
	if err != nil {
		return nil, "", err
	}
	authOpts, err := getAuthOpts(u, prd.http, prd.ssh)
	if err != nil {
		return nil, "", err
	}
	clientOpts := []gogit.ClientOption{gogit.WithDiskStorage()}
	if prd.http != nil && prd.http.InsecureHttpAllowed.ValueBool() {
		clientOpts = append(clientOpts, gogit.WithInsecureCredentialsOverHTTP())
	}

	tmpDir, err := os.MkdirTemp("", "terraform-provider-git")
	if err != nil {
		return nil, "", err
	}
	client, err := gogit.NewClient(tmpDir, authOpts, clientOpts...)
	if err != nil {
		return nil, tmpDir, fmt.Errorf("could not create git client: %w", err)
	}

	// When appending a timestamp, the target branch does not exist yet, so the
	// clone is performed from the base branch and the new branch is created
	// locally afterwards. Otherwise the configured branch is cloned directly.
	checkoutBranch := prd.branch
	if prd.append_timestamp {
		checkoutBranch = prd.base_branch
	}
	if checkoutBranch == "" {
		checkoutBranch = "main"
	}

	_, err = client.Clone(ctx, prd.url, repository.CloneConfig{CheckoutStrategy: repository.CheckoutStrategy{Branch: checkoutBranch}})
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, tmpDir, err
	}

	if prd.append_timestamp && prd.branch != "" {
		if err := client.SwitchBranch(ctx, prd.branch); err != nil {
			return nil, tmpDir, fmt.Errorf("could not create branch %q: %w", prd.branch, err)
		}
	}

	return client, tmpDir, nil
}

// detectDefaultBranch uses git ls-remote to discover the remote HEAD ref and
// returns the corresponding branch name. This avoids hardcoding "main"/"master".
func (prd *ProviderResourceData) detectDefaultBranch() (string, error) {
	u, err := url.Parse(prd.url)
	if err != nil {
		return "", fmt.Errorf("could not parse URL: %w", err)
	}
	authOpts, err := getAuthOpts(u, prd.http, prd.ssh)
	if err != nil {
		return "", fmt.Errorf("could not get auth options: %w", err)
	}

	authMethod, err := gogitAuthMethod(authOpts)
	if err != nil {
		return "", fmt.Errorf("could not build auth method: %w", err)
	}

	remote := gogitv5.NewRemote(memory.NewStorage(), &gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{prd.url},
	})

	listOpts := &gogitv5.ListOptions{
		Auth:     authMethod,
		CABundle: authOpts.CAFile,
	}
	if u.Scheme == "http" && (prd.http == nil || !prd.http.InsecureHttpAllowed.ValueBool()) {
		// Prevent leaking credentials over plain HTTP unless explicitly allowed.
		listOpts.Auth = nil
	}

	refs, err := remote.List(listOpts)
	if err != nil {
		return "", fmt.Errorf("could not list remote refs: %w", err)
	}

	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference {
			target := ref.Target().String()
			if branch, ok := strings.CutPrefix(target, "refs/heads/"); ok {
				return branch, nil
			}
		}
	}

	return "", fmt.Errorf("HEAD ref not found in remote refs")
}

// gogitAuthMethod converts a fluxcd git.AuthOptions into a go-git
// transport.AuthMethod suitable for use with go-git's Remote.List.
func gogitAuthMethod(opts *git.AuthOptions) (gogittransport.AuthMethod, error) {
	if opts == nil {
		return nil, nil
	}
	switch opts.Transport {
	case git.HTTP, git.HTTPS:
		if opts.Username != "" || opts.Password != "" {
			return &gogithttp.BasicAuth{
				Username: opts.Username,
				Password: opts.Password,
			}, nil
		}
		return nil, nil
	case git.SSH:
		pk, err := gogitssh.NewPublicKeys(opts.Username, opts.Identity, opts.Password)
		if err != nil {
			return nil, err
		}
		if len(opts.KnownHosts) > 0 {
			callback, algorithms, err := fluxknownhosts.New(opts.KnownHosts)
			if err != nil {
				return nil, err
			}
			pk.HostKeyCallback = callback
			pk.HostKeyAlgorithms = algorithms
		}
		return pk, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", opts.Transport)
	}
}

func isMissingRemoteRefError(err error) bool {
	errMessage := strings.ToLower(err.Error())
	return strings.Contains(errMessage, "couldn't find remote ref") ||
		strings.Contains(errMessage, "reference not found")
}

func getAuthOpts(u *url.URL, h *Http, s *Ssh) (*git.AuthOptions, error) {
	switch u.Scheme {
	case "http":
		return &git.AuthOptions{
			Transport: git.HTTP,
			Username:  h.Username.ValueString(),
			Password:  h.Password.ValueString(),
		}, nil
	case "https":
		return &git.AuthOptions{
			Transport: git.HTTPS,
			Username:  h.Username.ValueString(),
			Password:  h.Password.ValueString(),
			CAFile:    []byte(h.CertificateAuthority.ValueString()),
		}, nil
	case "ssh":
		if s.PrivateKey.ValueString() != "" {
			kh, err := sourcesecret.ScanHostKey(u.Host)
			if err != nil {
				return nil, err
			}
			return &git.AuthOptions{
				Transport:  git.SSH,
				Username:   s.Username.ValueString(),
				Password:   s.Password.ValueString(),
				Identity:   []byte(s.PrivateKey.ValueString()),
				KnownHosts: kh,
			}, nil
		}
		return nil, fmt.Errorf("ssh scheme cannot be used without private key")
	default:
		return nil, fmt.Errorf("scheme %q is not supported", u.Scheme)
	}
}
