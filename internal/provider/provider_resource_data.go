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
)

type ProviderResourceData struct {
	url            string
	branch         string
	ssh            *Ssh
	http           *Http
	commits        *Commits
	ignore_updates bool
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
	branch := prd.branch
	if branch == "" {
		branch = "main"
	}
	return prd.GetGitClientForBranch(ctx, branch)
}

func (prd *ProviderResourceData) GetGitClientForBranch(ctx context.Context, branch string) (*gogit.Client, error) {
	u, err := url.Parse(prd.url)
	if err != nil {
		return nil, err
	}
	authOpts, err := getAuthOpts(u, prd.http, prd.ssh)
	if err != nil {
		return nil, err
	}
	clientOpts := []gogit.ClientOption{gogit.WithDiskStorage()}
	if prd.http != nil && prd.http.InsecureHttpAllowed.ValueBool() {
		clientOpts = append(clientOpts, gogit.WithInsecureCredentialsOverHTTP())
	}

	newClient := func() (*gogit.Client, string, error) {
		tmpDir, err := os.MkdirTemp("", "terraform-provider-git")
		if err != nil {
			return nil, "", err
		}
		client, err := gogit.NewClient(tmpDir, authOpts, clientOpts...)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("could not create git client: %w", err)
		}
		return client, tmpDir, nil
	}

	client, tmpDir, err := newClient()
	if err != nil {
		return nil, err
	}
	_, cloneErr := client.Clone(ctx, prd.url, repository.CloneConfig{CheckoutStrategy: repository.CheckoutStrategy{Branch: branch}})
	if cloneErr == nil {
		return client, nil
	}

	// Only attempt the fallback when the branch simply does not exist yet on the
	// remote.  Auth errors, network errors, or a completely missing repository
	// should be surfaced immediately.
	if !isBranchNotFoundErr(cloneErr) {
		os.RemoveAll(tmpDir)
		return nil, cloneErr
	}

	// The branch doesn't exist on the remote yet (e.g. append_timestamp_to_branch
	// produced a brand-new name).  Clean up the failed clone and fall back to
	// cloning the default branch, then create and push the requested branch so
	// that subsequent git_repository_file operations can proceed.
	os.RemoveAll(tmpDir)

	fallbackClient, fallbackTmpDir, err := newClient()
	if err != nil {
		return nil, err
	}

	var fallbackCloneErr error
	for _, defaultBranch := range []string{"main", "master"} {
		_, fallbackCloneErr = fallbackClient.Clone(ctx, prd.url, repository.CloneConfig{
			CheckoutStrategy: repository.CheckoutStrategy{Branch: defaultBranch},
		})
		if fallbackCloneErr == nil {
			break
		}
	}
	if fallbackCloneErr != nil {
		os.RemoveAll(fallbackTmpDir)
		return nil, fmt.Errorf("branch %q not found and could not clone default branch as fallback: %w", branch, fallbackCloneErr)
	}

	if err := fallbackClient.SwitchBranch(ctx, branch); err != nil {
		os.RemoveAll(fallbackTmpDir)
		return nil, fmt.Errorf("failed to create local branch %q: %w", branch, err)
	}

	refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
	if err := fallbackClient.Push(ctx, repository.PushConfig{Refspecs: []string{refspec}}); err != nil {
		os.RemoveAll(fallbackTmpDir)
		return nil, fmt.Errorf("failed to push branch %q to remote: %w", branch, err)
	}

	return fallbackClient, nil
}

// isBranchNotFoundErr reports whether err indicates that a specific branch does
// not exist on the remote (as opposed to the entire repository being absent or
// an authentication/network failure).
func isBranchNotFoundErr(err error) bool {
	return strings.Contains(err.Error(), "couldn't find remote ref")
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
