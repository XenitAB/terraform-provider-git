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

type DynamicBranch struct {
	Prefix          types.String `tfsdk:"prefix"`
	Suffix          types.String `tfsdk:"suffix"`
	Base            types.String `tfsdk:"base"`
	TimestampFormat types.String `tfsdk:"timestamp_format"`
}

type GitProviderModel struct {
	Url           types.String   `tfsdk:"url"`
	Branch        types.String   `tfsdk:"branch"`
	DynamicBranch *DynamicBranch `tfsdk:"dynamic_branch"`
	Ssh           *Ssh           `tfsdk:"ssh"`
	Http          *Http          `tfsdk:"http"`
	Commits       *Commits       `tfsdk:"commits"`
	IgnoreUpdates types.Bool     `tfsdk:"ignore_updates"`
}

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
				Description: "Branchname to use for commits. Conflicts with `dynamic_branch`. If neither is set, `main` is used.",
				Optional:    true,
			},
			"dynamic_branch": schema.SingleNestedAttribute{
				Description: "Generate a unique branch name on every run and base it on an existing branch. Conflicts with `branch`.",
				Attributes: map[string]schema.Attribute{
					"prefix": schema.StringAttribute{
						Description: "String prepended to the generated timestamp, e.g. `terraform/`.",
						Optional:    true,
					},
					"suffix": schema.StringAttribute{
						Description: "String appended to the generated timestamp.",
						Optional:    true,
					},
					"base": schema.StringAttribute{
						Description: "Branch the new branch is created from. Defaults to `main`.",
						Optional:    true,
					},
					"timestamp_format": schema.StringAttribute{
						Description: "Go reference time layout used for the timestamp portion of the branch name. Defaults to `2006-01-02-15-04` (Year-Month-Day-Hour-Minute).",
						Optional:    true,
					},
				},
				Optional: true,
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

	branch, baseBranch, dynamic, err := resolveBranch(&data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Branch Configuration", err.Error())
		return
	}

	resp.ResourceData = &ProviderResourceData{
		url:            data.Url.ValueString(),
		branch:         branch,
		baseBranch:     baseBranch,
		dynamicBranch:  dynamic,
		ssh:            data.Ssh,
		http:           data.Http,
		commits:        newCommits(&data),
		ignore_updates: data.IgnoreUpdates.ValueBool(),
	}
}

// defaultBaseBranch is used as the branch new dynamic branches are based on, as
// well as the branch checked out when no branch is configured at all.
const defaultBaseBranch = "main"

// defaultTimestampFormat is the Go reference time layout used for the timestamp
// portion of a dynamic branch name (Year-Month-Day-Hour-Minute).
const defaultTimestampFormat = "2006-01-02-15-04"

// resolveBranch determines the branch to commit to. It returns the resolved
// branch name, the branch it should be based on, and whether the branch is
// dynamically generated and therefore may need to be created.
func resolveBranch(m *GitProviderModel) (branch string, base string, dynamic bool, err error) {
	hasStatic := m.Branch.ValueString() != ""
	hasDynamic := m.DynamicBranch != nil

	if hasStatic && hasDynamic {
		return "", "", false, fmt.Errorf("only one of \"branch\" or \"dynamic_branch\" may be set")
	}

	if hasDynamic {
		db := m.DynamicBranch

		base := db.Base.ValueString()
		if base == "" {
			base = defaultBaseBranch
		}

		format := db.TimestampFormat.ValueString()
		if format == "" {
			format = defaultTimestampFormat
		}

		timestamp := time.Now().UTC().Format(format)
		name := db.Prefix.ValueString() + timestamp + db.Suffix.ValueString()
		return name, base, true, nil
	}

	if hasStatic {
		return m.Branch.ValueString(), m.Branch.ValueString(), false, nil
	}

	return defaultBaseBranch, defaultBaseBranch, false, nil
}

func (p *GitProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRepositoryFileResource,
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
