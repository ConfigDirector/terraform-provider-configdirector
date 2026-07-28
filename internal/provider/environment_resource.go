package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
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

type EnvironmentModel struct {
	Id        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	Color     types.String `tfsdk:"color"`
	ProjectId types.String `tfsdk:"project_id"`
	Live      types.Bool   `tfsdk:"live"`
}

func (r *EnvironmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *EnvironmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			// project_id is a path parameter with no way for the API's OpenAPI
			// spec to convey that it's user-supplied rather than server-generated;
			// it's Required+RequiresReplace here since the API has no way to move
			// an environment between projects.
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project this environment belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"slug": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 150),
				},
			},
			"color": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"maroon", "red", "purple", "fuchsia", "green", "lime",
						"olive", "yellow", "navy", "blue", "teal", "aqua",
					),
				},
			},
			"live": schema.BoolAttribute{
				Required: true,
			},
		},
	}
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
		ID:    plan.Id.ValueString(),
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

// ImportState accepts project_id_or_slug/environment_id_or_slug. Both
// halves accept either form: the project, via resolveProjectID (there's no
// get-by-slug endpoint); the environment, so environments the API
// auto-creates for a new project (e.g. "test", "production") can be
// adopted via an import block without first having to look up their
// generated UUID.
func (r *EnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id_or_slug/environment_id_or_slug. Got: %q", req.ID),
		)
		return
	}
	identifier := parts[1]

	projectID, err := resolveProjectID(ctx, r.client, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Project", err.Error())
		return
	}

	envs, err := r.client.ListEnvironments(ctx, projectID)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Environments", err.Error())
		return
	}

	var environmentID string
	for _, e := range envs {
		if e.ID == identifier || e.Slug == identifier {
			environmentID = e.ID
			break
		}
	}
	if environmentID == "" {
		resp.Diagnostics.AddError(
			"Environment Not Found",
			fmt.Sprintf("No environment with id or slug %q was found in project %q.", identifier, projectID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), environmentID)...)
}
