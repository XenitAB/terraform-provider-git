package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/fluxcd/pkg/git/repository"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
)

const defaultTimestampFormat = "20060102150405"

type RepositoryBranchResourceModel struct {
	ID              types.String   `tfsdk:"id"`
	Name            types.String   `tfsdk:"name"`
	SourceBranch    types.String   `tfsdk:"source_branch"`
	AppendTimestamp types.Bool     `tfsdk:"append_timestamp"`
	TimestampFormat types.String   `tfsdk:"timestamp_format"`
	ComputedName    types.String   `tfsdk:"computed_name"`
	Timeouts        timeouts.Value `tfsdk:"timeouts"`
}

var _ resource.Resource = &RepositoryBranchResource{}

func NewRepositoryBranchResource() resource.Resource {
	return &RepositoryBranchResource{}
}

type RepositoryBranchResource struct {
	prd *ProviderResourceData
}

func (r *RepositoryBranchResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository_branch"
}

func (r *RepositoryBranchResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Repository branch resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Base name of the branch.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_branch": schema.StringAttribute{
				Optional:    true,
				Description: "Branch to create from. Defaults to the provider-level branch config or main.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"append_timestamp": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether to append a -YYYYMMDDHHMMSS timestamp suffix to the branch name.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"timestamp_format": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(defaultTimestampFormat),
				Description: "Go time package layout string for the timestamp suffix. Default: \"20060102150405\" (YYYYMMDDHHMMSS in 24-hour format).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"computed_name": schema.StringAttribute{
				Computed:    true,
				Description: "The final branch name after optional timestamp is appended.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"timeouts": timeouts.AttributesAll(ctx),
		},
	}
}

func (r *RepositoryBranchResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	prd, ok := req.ProviderData.(*ProviderResourceData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ProviderResourceData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.prd = prd
}

func (r *RepositoryBranchResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *RepositoryBranchResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	// Determine source branch, falling back to the provider default or "main".
	sourceBranch := data.SourceBranch.ValueString()
	if sourceBranch == "" {
		sourceBranch = r.prd.Branch(ctx)
	}
	if sourceBranch == "" {
		sourceBranch = "main"
	}

	// Compute the final branch name once at create time so it is stable.
	format := data.TimestampFormat.ValueString()
	if format == "" {
		format = defaultTimestampFormat
	}
	computedName := computeBranchName(data.Name.ValueString(), data.AppendTimestamp.ValueBool(), format)

	err := retry.RetryContext(ctx, createTimeout, func() *retry.RetryError {
		client, err := r.prd.GetGitClientForBranch(ctx, sourceBranch)
		if err != nil {
			return retry.NonRetryableError(fmt.Errorf("failed to clone repository from branch %q: %w", sourceBranch, err))
		}

		if err := client.SwitchBranch(ctx, computedName); err != nil {
			return retry.NonRetryableError(fmt.Errorf("failed to create branch %q: %w", computedName, err))
		}

		refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", computedName, computedName)
		if err := client.Push(ctx, repository.PushConfig{Refspecs: []string{refspec}}); err != nil {
			return retry.RetryableError(fmt.Errorf("failed to push branch %q: %w", computedName, err))
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError("Git Branch Create Error", err.Error())
		return
	}

	data.ComputedName = types.StringValue(computedName)
	data.ID = types.StringValue(computedName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryBranchResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *RepositoryBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, diags := data.Timeouts.Read(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	// Attempt to clone from the branch to verify it still exists on the remote.
	branchName := data.ComputedName.ValueString()
	_, err := r.prd.GetGitClientForExistingBranch(ctx, branchName)
	if err != nil {
		// The branch can no longer be reached; remove from state so Terraform
		// will recreate it on the next apply.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryBranchResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All mutable attributes that affect the branch identity carry RequiresReplace,
	// so Update is only reached for in-place changes to computed defaults.
	// Simply persist the plan values.
	var data *RepositoryBranchResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryBranchResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *RepositoryBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diags := data.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	branchName := data.ComputedName.ValueString()

	err := retry.RetryContext(ctx, deleteTimeout, func() *retry.RetryError {
		client, err := r.prd.GetGitClient(ctx)
		if err != nil {
			return retry.NonRetryableError(fmt.Errorf("failed to clone repository: %w", err))
		}

		// An empty source in the refspec instructs the remote to delete the ref.
		deleteRefspec := fmt.Sprintf(":refs/heads/%s", branchName)
		if err := client.Push(ctx, repository.PushConfig{Refspecs: []string{deleteRefspec}}); err != nil {
			return retry.RetryableError(fmt.Errorf("failed to delete remote branch %q: %w", branchName, err))
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError("Git Branch Delete Error", err.Error())
	}
}

// computeBranchName returns the final branch name, optionally appending a
// timestamp suffix using the provided Go time layout. The timestamp is always
// formatted in UTC so that the result is consistent across time zones and uses
// 24-hour clock values (Go's reference hour is 15, which is 24-hour format).
func computeBranchName(name string, appendTimestamp bool, format string) string {
	if !appendTimestamp {
		return name
	}
	if format == "" {
		format = defaultTimestampFormat
	}
	return name + "-" + time.Now().UTC().Format(format)
}
