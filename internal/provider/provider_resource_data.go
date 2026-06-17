package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/fluxcd/flux2/pkg/manifestgen/sourcesecret"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
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
	tmpDir, err := os.MkdirTemp("", "terraform-provider-git")
	if err != nil {
		return nil, err
	}
	client, err := gogit.NewClient(tmpDir, authOpts, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("could not create git client: %w", err)
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
		return nil, err
	}

	if prd.append_timestamp && prd.branch != "" {
		if err := client.SwitchBranch(ctx, prd.branch); err != nil {
			return nil, fmt.Errorf("could not create branch %q: %w", prd.branch, err)
		}
	}

	return client, err
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
