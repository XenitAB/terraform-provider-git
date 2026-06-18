package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type Ssh struct {
	Username   types.String `tfsdk:"username"`
	Password   types.String `tfsdk:"password"`
	PrivateKey types.String `tfsdk:"private_key"`
}

type Http struct {
	Username             types.String `tfsdk:"username"`
	Password             types.String `tfsdk:"password"`
	InsecureHttpAllowed  types.Bool   `tfsdk:"allow_insecure_http"`
	CertificateAuthority types.String `tfsdk:"certificate_authority"`
}

type Commits struct {
	AuthorName  types.String `tfsdk:"author_name"`
	AuthorEmail types.String `tfsdk:"author_email"`
	Message     types.String `tfsdk:"message"`
}

type GitProviderModel struct {
	Url           types.String `tfsdk:"url"`
	Branch        types.String `tfsdk:"branch"`
	BaseBranch    types.String `tfsdk:"base_branch"`
	BranchSuffix  types.String `tfsdk:"branch_suffix"`
	Ssh           *Ssh         `tfsdk:"ssh"`
	Http          *Http        `tfsdk:"http"`
	Commits       *Commits     `tfsdk:"commits"`
	IgnoreUpdates types.Bool   `tfsdk:"ignore_updates"`
}

func (c *Commits) Author() string {
	if c.AuthorName.ValueString() == "" {
		return "Terraform Provider Git"
	}
	return c.AuthorName.ValueString()
}

func (c *Commits) Email() string {
	return c.AuthorName.ValueString()
}

func (c *Commits) Msg() string {
	if c.Message.ValueString() == "" {
		return "Write file with Terraform Provider Git"
	}
	return c.Message.ValueString()
}

func newCommits(m *GitProviderModel) *Commits {
	c := m.Commits
	if c == nil {
		c = &Commits{
			AuthorName: basetypes.NewStringValue("Terraform Provider Git"),
			Message:    basetypes.NewStringValue("Write file with Terraform Provider Git."),
		}
	}
	return c
}

// resolveConfiguredBranch computes the branch the provider should use for
// commits, taking an optional caller-supplied suffix into account. When suffix
// is non-empty and a branch name is configured, it is appended as
// "<branch>-<suffix>". Because the suffix is supplied from the configuration
// (for example a value generated once per run by a resource persisted in
// state), the resulting branch name is identical across every
// plan/apply/refresh phase of a run. When no suffix is given the branch name is
// used as-is.
func resolveConfiguredBranch(branch, suffix string) string {
	if branch != "" && suffix != "" {
		return fmt.Sprintf("%s-%s", branch, suffix)
	}
	return branch
}

var _ provider.Provider = &GitProvider{}

type GitProvider struct {
	version string
}

func (p *GitProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "git"
	resp.Version = p.version
}

func (p *GitProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Required: true,
			},
			"branch": schema.StringAttribute{
				Description: "Branchname to use for commits. When combined with branch_suffix the resulting branch is \"<branch>-<branch_suffix>\".",
				Optional:    true,
			},
			"base_branch": schema.StringAttribute{
				Description: "Branch to base a new branch on when the configured branch does not yet exist remotely (it is the first fallback source from which the new branch is created). Defaults to \"main\".",
				Optional:    true,
			},
			"branch_suffix": schema.StringAttribute{
				Description: "Stable suffix appended to branch as \"<branch>-<branch_suffix>\". The value is supplied by you and must be the same for every plan/apply/refresh phase of a run, so the resulting branch name is identical across all phases. Generate it once inside the configuration with a resource that persists its value in state (for example time_static for a date) and reference that value here.",
				Optional:    true,
			},
			"ssh": schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"username": schema.StringAttribute{
						Description: "Username for Git SSH server.",
						Optional:    true,
					},
					"password": schema.StringAttribute{
						Description: "Password for private key.",
						Optional:    true,
						Sensitive:   true,
					},
					"private_key": schema.StringAttribute{
						Description: "Private key used for authenticating to the Git SSH server.",
						Optional:    true,
						Sensitive:   true,
					},
				},
				Optional: true,
			},
			"http": schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"username": schema.StringAttribute{
						Description: "Username for basic authentication.",
						Optional:    true,
					},
					"password": schema.StringAttribute{
						Description: "Password for basic authentication.",
						Optional:    true,
						Sensitive:   true,
					},
					"allow_insecure_http": schema.BoolAttribute{
						Description: "Allows http Git url connections.",
						Optional:    true,
					},
					"certificate_authority": schema.StringAttribute{
						Description: "Certificate authority to validate self-signed certificates.",
						Optional:    true,
					},
				},
				Optional: true,
			},
			"commits": schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"author_name": schema.StringAttribute{
						Description: "Author name for commits.",
						Optional:    true,
					},
					"author_email": schema.StringAttribute{
						Description: "Author email for commits.",
						Optional:    true,
					},
					"message": schema.StringAttribute{
						Description: "Commit message.",
						Optional:    true,
					},
				},
				Optional: true,
			},
			"ignore_updates": schema.BoolAttribute{
				Optional:    true,
				Description: "If true, any updates to resources of type git_repository_file will be ignored.",
			},
		},
	}
}

func (p *GitProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data GitProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	branch := resolveConfiguredBranch(data.Branch.ValueString(), data.BranchSuffix.ValueString())

	resp.ResourceData = &ProviderResourceData{
		url:            data.Url.ValueString(),
		branch:         branch,
		base_branch:    data.BaseBranch.ValueString(),
		ssh:            data.Ssh,
		http:           data.Http,
		commits:        newCommits(&data),
		ignore_updates: data.IgnoreUpdates.ValueBool(),
	}
}

func (p *GitProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRepositoryFileResource,
		NewRepositoryBranchResource,
	}
}

func (p *GitProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GitProvider{
			version: version,
		}
	}
}
