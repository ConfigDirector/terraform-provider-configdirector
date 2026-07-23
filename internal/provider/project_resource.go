package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &ProjectResource{}
	_ resource.ResourceWithImportState = &ProjectResource{}
)

func NewProjectResource() resource.Resource {
	return &ProjectResource{}
}

type ProjectResource struct {
	client *client.Client
}

type ProjectModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Slug           types.String `tfsdk:"slug"`
	Environments   types.List   `tfsdk:"environments"`
}

func (r *ProjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

// The ConfigDirector API has no update endpoint for projects, so name/slug
// changes must force a replacement rather than an in-place update.
//
// "environments" is computed-only: Terraform can't register the
// project's auto-created "test"/"production" environments as independently
// managed configdirector_environment resources on its own (a provider's
// Create can't inject new resource instances into another address), so
// they're surfaced here as read-only data instead.
func (r *ProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"organization_id": schema.StringAttribute{
				Computed: true,
			},
			"slug": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[\da-z]+(?:[-_][\da-z]+)*$`), ""),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"environments": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"color":      schema.StringAttribute{Computed: true},
						"id":         schema.StringAttribute{Computed: true},
						"live":       schema.BoolAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"project_id": schema.StringAttribute{Computed: true},
						"slug":       schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (r *ProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func projectToModel(ctx context.Context, p *client.Project, m *ProjectModel) error {
	m.Id = stringValue(p.ID)
	m.Name = stringValue(p.Name)
	m.Slug = stringValue(p.Slug)
	m.OrganizationId = stringValue(p.OrganizationID)

	envList, diags := environmentSummaryListValue(ctx, p.Environments)
	if diags.HasError() {
		return fmt.Errorf("%v", diags)
	}
	m.Environments = envList
	return nil
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.client.CreateProject(ctx, client.CreateProjectRequest{
		Name: plan.Name.ValueString(),
		Slug: plan.Slug.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Project", err.Error())
		return
	}

	if err := projectToModel(ctx, project, &plan); err != nil {
		resp.Diagnostics.AddError("Error Processing Project Response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.client.GetProject(ctx, state.Id.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Project", err.Error())
		return
	}

	if err := projectToModel(ctx, project, &state); err != nil {
		resp.Diagnostics.AddError("Error Processing Project Response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable in practice: name/slug are the only user-settable
// attributes and both require replacement. It's implemented defensively in
// case the schema gains an in-place-updatable attribute later.
func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ProjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ProjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteProject(ctx, state.Id.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Project", err.Error())
	}
}

// ImportState accepts either the project's id or its slug. Read() always
// looks projects up by id, so a slug identifier is resolved to an id here
// via the project list (there's no get-by-slug endpoint).
func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projects, err := r.client.ListProjects(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Projects", err.Error())
		return
	}

	var projectID string
	for _, p := range projects {
		if p.ID == req.ID || p.Slug == req.ID {
			projectID = p.ID
			break
		}
	}
	if projectID == "" {
		resp.Diagnostics.AddError(
			"Project Not Found",
			fmt.Sprintf("No project with id or slug %q was found.", req.ID),
		)
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root("id"), resource.ImportStateRequest{ID: projectID}, resp)
}
