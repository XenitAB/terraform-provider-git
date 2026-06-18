package provider

import (
	"context"
	"fmt"
	"time"

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
	Url                     types.String `tfsdk:"url"`
	Branch                  types.String `tfsdk:"branch"`
	BaseBranch              types.String `tfsdk:"base_branch"`
	BranchSuffix            types.String `tfsdk:"branch_suffix"`
	AppendTimestampToBranch types.Bool   `tfsdk:"append_timestamp_to_branch"`
	Ssh                     *Ssh         `tfsdk:"ssh"`
	Http                    *Http        `tfsdk:"http"`
	Commits                 *Commits     `tfsdk:"commits"`
	IgnoreUpdates           types.Bool   `tfsdk:"ignore_updates"`
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

// branchTimestampSuffix returns a timestamp suffix in the format
// YYYYMMDDHHMMSSmmm (UTC), where mmm are the milliseconds expressed with three
// digits. It is used to make every provider run target a unique branch.
func branchTimestampSuffix(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%s%03d", t.Format("20060102150405"), t.Nanosecond()/int(time.Millisecond))
}

// resolveBranch computes the branch the provider should use for commits. When
// appendTimestamp is true and a branch name is configured, a unique timestamp
// suffix is appended so that each run lands on its own branch.
func resolveBranch(branch string, appendTimestamp bool, now time.Time) string {
	if appendTimestamp && branch != "" {
		return fmt.Sprintf("%s-%s", branch, branchTimestampSuffix(now))
	}
	return branch
}

// resolveConfiguredBranch computes the branch the provider should use for
// commits, taking an optional caller-supplied suffix into account. When suffix
// is non-empty and a branch name is configured, it is appended as
// "<branch>-<suffix>" and takes precedence over append_timestamp_to_branch.
// Because the suffix is supplied from outside the provider (for example a
// pipeline-generated value passed once per run via a variable), the resulting
// branch name is identical across every plan/apply/refresh phase of a run,
// avoiding the instability of append_timestamp_to_branch. When no suffix is
// given the legacy append_timestamp_to_branch behaviour is preserved.
func resolveConfiguredBranch(branch, suffix string, appendTimestamp bool, now time.Time) string {
	if branch != "" && suffix != "" {
		return fmt.Sprintf("%s-%s", branch, suffix)
	}
	return resolveBranch(branch, appendTimestamp, now)
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
				Description: "Branchname to use for commits. When append_timestamp_to_branch is true this is used as the prefix of the branch that is created.",
				Optional:    true,
			},
			"base_branch": schema.StringAttribute{
				Description: "Branch to base a new branch on when append_timestamp_to_branch is true. Defaults to \"main\".",
				Optional:    true,
			},
			"branch_suffix": schema.StringAttribute{
				Description: "Stable suffix appended to branch as \"<branch>-<branch_suffix>\". Unlike append_timestamp_to_branch, this value is supplied by you and must be the same for every plan/apply/refresh phase of a run, so the resulting branch name is identical across all phases. Generate it once inside the configuration with a resource that persists its value in state (for example random_id/random_pet, or time_static for a date) and reference that value here; it does not need to be a date, any stable id that relates the run to its branch works. Takes precedence over append_timestamp_to_branch.",
				Optional:    true,
			},
			"append_timestamp_to_branch": schema.BoolAttribute{
				Description:        "If true, automatically appends a -YYYYMMDDHHMMSS timestamp suffix (24-hour clock) to the branch name.",
				DeprecationMessage: "append_timestamp_to_branch is deprecated and unreliable: the suffix is recomputed every time the provider is configured (each plan/apply/refresh phase), so the resolved branch name is not stable and is never persisted in state. Use a git_repository_branch resource with append_timestamp = true and reference its computed_name from git_repository_file.branch instead.",
				Optional:           true,
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

	appendTimestamp := data.AppendTimestampToBranch.ValueBool()
	branch := resolveConfiguredBranch(data.Branch.ValueString(), data.BranchSuffix.ValueString(), appendTimestamp, time.Now())

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

func configuredBranchName(branch string, appendTimestamp bool, now func() time.Time) string {
	if !appendTimestamp || branch == "" {
		return branch
	}

	return branch + "-" + now().Format("20060102150405")
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
