package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &EnvironmentResource{}
	_ resource.ResourceWithImportState = &EnvironmentResource{}
)

func NewEnvironmentResource() resource.Resource {
	return &EnvironmentResource{}
}

type EnvironmentResource struct {
	client *client.Client
}

func (r *EnvironmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

// project_id is a path parameter the OpenAPI codegen has no way to know is
// user-supplied rather than server-generated, so it comes out of codegen as
// computed-only; it's overridden here to Required+RequiresReplace since the
// API has no way to move an environment between projects.
func (r *EnvironmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	s := EnvironmentResourceSchema(ctx)

	s.Attributes["project_id"] = schema.StringAttribute{
		Required:      true,
		Description:   "ID of the project this environment belongs to.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
	s.Attributes["id"] = schema.StringAttribute{
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
	slugAttr := s.Attributes["slug"].(schema.StringAttribute)
	slugAttr.PlanModifiers = append(slugAttr.PlanModifiers, stringplanmodifier.RequiresReplace())
	s.Attributes["slug"] = slugAttr

	resp.Schema = s
}

func (r *EnvironmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func environmentToModel(e *client.Environment, m *EnvironmentModel) {
	m.Id = stringValue(e.ID)
	m.Name = stringValue(e.Name)
	m.Slug = stringValue(e.Slug)
	m.Color = stringValue(e.Color)
	m.ProjectId = stringValue(e.ProjectID)
	m.CreatedAt = stringValue(e.CreatedAt)
	m.UpdatedAt = stringValue(e.UpdatedAt)
	m.Live = boolValue(e.Live)
}

func (r *EnvironmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan EnvironmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.CreateEnvironment(ctx, plan.ProjectId.ValueString(), client.CreateEnvironmentRequest{
		Name:  plan.Name.ValueString(),
		Slug:  plan.Slug.ValueString(),
		Color: plan.Color.ValueString(),
		Live:  plan.Live.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Environment", err.Error())
		return
	}

	environmentToModel(env, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state EnvironmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.GetEnvironment(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Environment", err.Error())
		return
	}

	environmentToModel(env, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *EnvironmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan EnvironmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.UpdateEnvironment(ctx, plan.ProjectId.ValueString(), plan.Id.ValueString(), client.UpdateEnvironmentRequest{
		Name:  plan.Name.ValueString(),
		Slug:  plan.Slug.ValueString(),
		Color: plan.Color.ValueString(),
		Live:  plan.Live.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Environment", err.Error())
		return
	}

	environmentToModel(env, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state EnvironmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteEnvironment(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Environment", err.Error())
	}
}

func (r *EnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id/environment_id. Got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
