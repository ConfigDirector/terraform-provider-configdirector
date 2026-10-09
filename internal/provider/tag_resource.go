package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                   = &tagResource{}
	_ resource.ResourceWithImportState    = &tagResource{}
	_ resource.ResourceWithValidateConfig = &tagResource{}
)

func newTagResource() resource.Resource {
	return &tagResource{}
}

type tagResource struct {
	client *client.Client
}

type tagModel struct {
	Id        types.String `tfsdk:"id"`
	ProjectId types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
}

func (r *tagResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *tagResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A shared tag in a project's tag catalog. Renaming changes the name wherever the tag is used while preserving its ID. Creating a tag does not assign it to configs or affect evaluation. Deleting a shared tag removes its assignments from active and archived configs.\n\n" +
			"Requires the tags:read, tags:create, tags:update, and tags:delete API token scopes. Import with project_id_or_slug/tag_name: only the first slash separates the project from the literal name, and names must not be URL-encoded. Importing by project slug additionally needs projects:read; importing by project UUID only needs tags:read.\n\n" +
			"Refresh detects remote renames by stable ID, and reconciliation restores the declared name. A remotely deleted tag is removed from state so it can be recreated. Deleting an already-missing tag succeeds. The catalog API must be deployed before using this resource; config assignment/filter work and dashboard feature flags do not affect its availability.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The tag's stable ID, preserved across renames.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project that owns this tag. Changing the project replaces the tag.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The tag's display name, 1–50 characters after trimming. Spaces and punctuation are allowed. Names are case-insensitively unique within the project. Changing the name renames the existing tag in place.",
			},
		},
	}
}

func (r *tagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *tagResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() || name.IsNull() || name.IsUnknown() {
		return
	}

	length := 0
	for _, character := range trimTagName(name.ValueString()) {
		length += utf16.RuneLen(character)
		if length > 50 {
			break
		}
	}
	if length < 1 || length > 50 {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid Tag Name", "Use a tag name with 1–50 characters after trimming leading and trailing whitespace. Spaces and punctuation are allowed.")
	}
}

func tagToModel(tag *client.Tag, model *tagModel) {
	model.Id = stringValue(tag.ID)
	model.ProjectId = stringValue(tag.ProjectID)
	preservesDeclaredName := !model.Name.IsNull() && !model.Name.IsUnknown() && trimTagName(model.Name.ValueString()) == tag.Name
	if !preservesDeclaredName {
		model.Name = stringValue(tag.Name)
	}
}

func (r *tagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.client.CreateTag(ctx, plan.ProjectId.ValueString(), client.TagNameRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Tag", tagOperationError(err, "tags:create"))
		return
	}

	tagToModel(tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.client.GetTag(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Tag", tagOperationError(err, "tags:read"))
		return
	}

	tagToModel(tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *tagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.client.UpdateTag(ctx, state.ProjectId.ValueString(), state.Id.ValueString(), client.TagNameRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Tag", tagOperationError(err, "tags:update"))
		return
	}

	tagToModel(tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteTag(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Tag", tagOperationError(err, "tags:delete"))
	}
}

func (r *tagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	project, name, found := strings.Cut(req.ID, "/")
	if !found || project == "" || trimTagName(name) == "" {
		resp.Diagnostics.AddError("Unexpected Import Identifier", fmt.Sprintf("Expected import identifier with format: project_id_or_slug/tag_name. Got: %q", req.ID))
		return
	}

	projectID := project
	projectUUID, err := uuid.Parse(project)
	isProjectUUID := err == nil && strings.EqualFold(projectUUID.String(), project)
	if isProjectUUID {
		projectID = projectUUID.String()
	} else {
		projectID, err = resolveProjectID(ctx, r.client, project)
		if err != nil {
			resp.Diagnostics.AddError("Error Resolving Project", err.Error())
			return
		}
	}

	tag, err := r.client.GetTagByName(ctx, projectID, name)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Tag", tagOperationError(err, "tags:read"))
		return
	}

	var state tagModel
	tagToModel(tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func tagOperationError(err error, permission string) string {
	var apiError *client.APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case 403:
			return fmt.Sprintf("%s Check that the API token grants %s for this project.", err, permission)
		case 400:
			return fmt.Sprintf("%s Use a name with 1–50 characters after trimming, unique within the project regardless of casing. To adopt an existing tag, import it using project_id_or_slug/tag_name; to rename, choose an unused name.", err)
		case 404:
			return fmt.Sprintf("%s Check that the tag exists in the project's catalog. Import names are literal and must not be URL-encoded.", err)
		}
	}
	return err.Error()
}

func trimTagName(name string) string {
	return strings.TrimFunc(name, func(character rune) bool {
		return character == '\ufeff' || (character != '\u0085' && unicode.IsSpace(character))
	})
}
