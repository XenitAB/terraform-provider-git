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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
				Computed: true,
				Default:  stringdefault.StaticString("main"),
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

	files := map[string]io.Reader{
		data.Path.ValueString(): strings.NewReader(data.Content.ValueString()),
	}
	commit := git.Commit{
		Message: data.Message.ValueString(),
		Author: git.Signature{
			Name:  data.AuthorName.ValueString(),
			Email: data.AuthorEmail.ValueString(),
		},
	}
	err := retry.RetryContext(ctx, createTimeout, func() *retry.RetryError {
		client, err := r.prd.GetGitClient(ctx, data.Branch.ValueString())
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
		err = client.Push(ctx)
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

	readTimeout, diags := data.Timeouts.Read(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	client, err := r.prd.GetGitClient(ctx, data.Branch.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Git Client Error", err.Error())
		return
	}
	absPath := filepath.Join(client.Path(), data.ID.ValueString())
	b, err := os.ReadFile(absPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		diags = resp.State.SetAttribute(ctx, path.Root("id"), "")
		resp.Diagnostics.Append(diags...)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("File Read Error", err.Error())
		return
	}
	data.Path = data.ID
	data.Content = types.StringValue(string(b))

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

	files := map[string]io.Reader{
		data.Path.ValueString(): strings.NewReader(data.Content.ValueString()),
	}
	commit := git.Commit{
		Message: data.Message.ValueString(),
		Author: git.Signature{
			Name:  data.AuthorName.ValueString(),
			Email: data.AuthorEmail.ValueString(),
		},
	}
	err := retry.RetryContext(ctx, updateTimeout, func() *retry.RetryError {
		client, err := r.prd.GetGitClient(ctx, data.Branch.ValueString())
		if err != nil {
			return retry.NonRetryableError(err)
		}
		_, err = client.Commit(commit, repository.WithFiles(files))
		if err != nil {
			return retry.NonRetryableError(err)
		}
		err = client.Push(ctx)
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
		client, err := r.prd.GetGitClient(ctx, data.Branch.ValueString())
		if err != nil {
			return retry.NonRetryableError(err)
		}
		path := filepath.Join(client.Path(), data.Path.ValueString())
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			tflog.Debug(ctx, "Skipping file removal as the file does not exist", map[string]interface{}{"path": path})
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
		err = client.Push(ctx)
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
	diags := resp.State.SetAttribute(ctx, path.Root("branch"), b)
	resp.Diagnostics.Append(diags...)
	diags = resp.State.SetAttribute(ctx, path.Root("id"), p)
	resp.Diagnostics.Append(diags...)
}
