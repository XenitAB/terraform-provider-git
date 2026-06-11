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
	client, tmpDir, err := prd.getGitClientForExistingBranch(ctx, branch)
	if err == nil {
		return client, nil
	}
	if !isMissingRemoteRefError(err) {
		return nil, err
	}

	os.RemoveAll(tmpDir)

	for _, fallbackBranch := range []string{"main", "master"} {
		fallbackClient, fallbackTmpDir, fallbackErr := prd.getGitClientForExistingBranch(ctx, fallbackBranch)
		if fallbackErr != nil {
			os.RemoveAll(fallbackTmpDir)
			continue
		}

		if err := fallbackClient.SwitchBranch(ctx, branch); err != nil {
			os.RemoveAll(fallbackTmpDir)
			return nil, err
		}

		refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
		if err := fallbackClient.Push(ctx, repository.PushConfig{Refspecs: []string{refspec}}); err != nil {
			os.RemoveAll(fallbackTmpDir)
			return nil, err
		}

		return fallbackClient, nil
	}

	return nil, err
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
		os.RemoveAll(tmpDir)
		return nil, tmpDir, fmt.Errorf("could not create git client: %w", err)
	}
	_, err = client.Clone(ctx, prd.url, repository.CloneConfig{CheckoutStrategy: repository.CheckoutStrategy{Branch: branch}})
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, tmpDir, err
	}
	return client, tmpDir, nil
}

func (prd *ProviderResourceData) GetGitClientForExistingBranch(ctx context.Context, branch string) (*gogit.Client, error) {
	client, _, err := prd.getGitClientForExistingBranch(ctx, branch)
	return client, err
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
