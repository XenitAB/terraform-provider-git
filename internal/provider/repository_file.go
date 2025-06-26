package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/repository"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
)

type RepositoryFileResourceModel struct {
	ID               types.String   `tfsdk:"id"`
	Branch           types.String   `tfsdk:"branch"`
	Path             types.String   `tfsdk:"path"`
	Content          types.String   `tfsdk:"content"`
	OverrideOnCreate types.Bool     `tfsdk:"override_on_create"`
	AuthorName       types.String   `tfsdk:"author_name"`
	AuthorEmail      types.String   `tfsdk:"author_email"`
	Message          types.String   `tfsdk:"message"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

var _ resource.Resource = &RepositoryFileResource{}
var _ resource.ResourceWithImportState = &RepositoryFileResource{}

func NewRepositoryFileResource() resource.Resource {
	return &RepositoryFileResource{}
}

type useStateIfUpdatesShouldBeIgnored struct{}

func (m useStateIfUpdatesShouldBeIgnored) Description(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change."
}

func (m useStateIfUpdatesShouldBeIgnored) MarkdownDescription(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change."
}

func (m useStateIfUpdatesShouldBeIgnored) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Do nothing if there is no state value.
	if req.StateValue.IsNull() {
		tflog.Debug(ctx, "StateValue is null", map[string]interface{}{})
		return
	}

	// Do nothing if there is an unknown configuration value, otherwise interpolation gets messed up.
	if req.ConfigValue.IsUnknown() {
		tflog.Debug(ctx, "ConfigValue is null", map[string]interface{}{})
		return
	}

	ignore, diag := req.Private.GetKey(ctx, "IgnoreUpdates")
	resp.Diagnostics.Append(diag...)
	if resp.Diagnostics.HasError() {
		return
	}

	if strings.EqualFold(string(ignore), "true") {
		if !req.ConfigValue.Equal(req.StateValue) {
			tflog.Debug(ctx, "Using state instead of plan value.", map[string]interface{}{})
			resp.PlanValue = req.StateValue
		}
	}
}

func UseStateIfUpdatesShouldBeIgnored() planmodifier.String {
	return useStateIfUpdatesShouldBeIgnored{}
}

type RepositoryFileResource struct {
	prd *ProviderResourceData
}

func (r *RepositoryFileResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository_file"
}

func (r *RepositoryFileResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Repository file resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"branch": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"path": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"content": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					UseStateIfUpdatesShouldBeIgnored(),
				},
			},
			"override_on_create": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{},
			},
			"author_name": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("Terraform Provider Git"),
			},
			"author_email": schema.StringAttribute{
				Optional: true,
			},
			"message": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("Write file with Terraform Provider Git."),
			},
			"timeouts": timeouts.AttributesAll(ctx),
		},
	}
}

func (r *RepositoryFileResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RepositoryFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *RepositoryFileResourceModel
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

	commit := git.Commit{
		Message: data.Message.ValueString(),
		Author: git.Signature{
			Name:  data.AuthorName.ValueString(),
			Email: data.AuthorEmail.ValueString(),
		},
	}

	err := retry.RetryContext(ctx, createTimeout, func() *retry.RetryError {
		files := map[string]io.Reader{
			data.Path.ValueString(): strings.NewReader(data.Content.ValueString()),
		}

		branch := r.prd.branch
		if branch == "" {
			branch = data.Branch.ValueString()
		}

		client, err := r.prd.GetGitClient(ctx, branch)
		if err != nil {
			return retry.NonRetryableError(err)
		}

		path := filepath.Join(client.Path(), data.Path.ValueString())
		_, err = os.Stat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return retry.NonRetryableError(err)
		}
		if err == nil && !data.OverrideOnCreate.ValueBool() {
			return retry.NonRetryableError(fmt.Errorf("cannot override existing file"))
		}

		_, err = client.Commit(commit, repository.WithFiles(files))
		if err != nil {
			return retry.NonRetryableError(err)
		}

		err = client.Push(ctx, repository.PushConfig{})
		if err != nil {
			return retry.RetryableError(err)
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError("Git File Create Error", err.Error())
		return
	}

	data.ID = data.Path
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *RepositoryFileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.prd.IgnoreUpdates(ctx) {
		tflog.Debug(ctx, "Provider is configured to ignore updates. The git file will not be read.", map[string]interface{}{})
		req.Private.SetKey(ctx, "IgnoreUpdates", []byte("true"))
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}
	req.Private.SetKey(ctx, "IgnoreUpdates", []byte("false"))

	readTimeout, diags := data.Timeouts.Read(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	r.ReadFile(ctx, data, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryFileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *RepositoryFileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diags := data.Timeouts.Update(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	commit := git.Commit{
		Message: data.Message.ValueString(),
		Author: git.Signature{
			Name:  data.AuthorName.ValueString(),
			Email: data.AuthorEmail.ValueString(),
		},
	}

	err := retry.RetryContext(ctx, updateTimeout, func() *retry.RetryError {
		branch := r.prd.branch
		if branch == "" {
			branch = data.Branch.ValueString()
		}

		client, err := r.prd.GetGitClient(ctx, branch)
		if err != nil {
			return retry.NonRetryableError(err)
		}

		path := filepath.Join(client.Path(), data.Path.ValueString())
		if _, exists := FileExists(path); !exists {
			return retry.NonRetryableError(errors.New("File Doesn't Exist"))
		}

		files := map[string]io.Reader{
			data.Path.ValueString(): strings.NewReader(data.Content.ValueString()),
		}

		_, err = client.Commit(commit, repository.WithFiles(files))
		if err != nil {
			return retry.NonRetryableError(err)
		}

		err = client.Push(ctx, repository.PushConfig{})
		if err != nil {
			return retry.RetryableError(err)
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError("Git File Update Error", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RepositoryFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *RepositoryFileResourceModel
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

	commit := git.Commit{
		Message: data.Message.ValueString(),
		Author: git.Signature{
			Name:  data.AuthorName.ValueString(),
			Email: data.AuthorEmail.ValueString(),
		},
	}

	err := retry.RetryContext(ctx, deleteTimeout, func() *retry.RetryError {
		branch := r.prd.branch
		if branch == "" {
			branch = data.Branch.ValueString()
		}

		client, err := r.prd.GetGitClient(ctx, branch)
		if err != nil {
			return retry.NonRetryableError(err)
		}

		path := filepath.Join(client.Path(), data.Path.ValueString())
		if _, exists := FileExists(path); !exists {
			tflog.Debug(ctx, "Skipping file removal as the file doesn't exist", map[string]interface{}{"path": path})
			return nil
		}

		err = os.Remove(path)
		if err != nil {
			return retry.NonRetryableError(err)
		}

		_, err = client.Commit(commit)
		if err != nil {
			return retry.NonRetryableError(err)
		}

		err = client.Push(ctx, repository.PushConfig{})
		if err != nil {
			return retry.RetryableError(err)
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError("Git File Remove Error", err.Error())
		return
	}
}

func (r *RepositoryFileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	b, p, ok := strings.Cut(req.ID, ":")
	if !ok {
		resp.Diagnostics.AddError("Invalid ID", "Expected id to have format branch:path")
	}

	data := &RepositoryFileResourceModel{
		ID:               basetypes.NewStringValue(p),
		Branch:           basetypes.NewStringValue(b),
		OverrideOnCreate: basetypes.NewBoolValue(true),
		AuthorName:       basetypes.NewStringValue("Terraform Provider Git"),
		Message:          basetypes.NewStringValue("Write file with Terraform Provider Git."),
	}

	importTimeout, diags := data.Timeouts.Read(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	r.ReadFile(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.SetAttribute(ctx, path.Root("branch"), data.Branch.ValueString())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("id"), data.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("path"), data.Path.ValueString())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("content"), data.Content.ValueString())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("override_on_create"), data.OverrideOnCreate.ValueBool())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("author_name"), data.AuthorName.ValueString())
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("message"), data.Message.ValueString())
	resp.Diagnostics.Append(diags...)
}

func (r *RepositoryFileResource) ReadFile(ctx context.Context, data *RepositoryFileResourceModel, diags *diag.Diagnostics) {
	client, err := r.prd.GetGitClient(ctx, data.Branch.ValueString())
	if err != nil {
		diags.AddError("Git Client Error", err.Error())
		return
	}

	path := filepath.Join(client.Path(), data.ID.ValueString())
	if err, exists := FileExists(path); !exists {
		diags.AddError("File Doesn't Exist", err.Error())
		return
	}

	b, err := os.ReadFile(path)
	if err != nil {
		diags.AddError("Error Reading File", err.Error())
		return
	}

	data.Path = data.ID
	data.Content = types.StringValue(string(b))
}

func FileExists(path string) (error, bool) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return err, false
	}
	if info.IsDir() {
		return errors.New("file is a directory"), false
	}
	return nil, true
}
