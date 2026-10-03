package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &SegmentResource{}
	_ resource.ResourceWithImportState = &SegmentResource{}
)

func NewSegmentResource() resource.Resource {
	return &SegmentResource{}
}

type SegmentResource struct {
	client *client.Client
}

type SegmentModel struct {
	Id        types.String  `tfsdk:"id"`
	ProjectId types.String  `tfsdk:"project_id"`
	Key       types.String  `tfsdk:"key"`
	Name      types.String  `tfsdk:"name"`
	Kind      types.String  `tfsdk:"kind"`
	Groups    types.Dynamic `tfsdk:"groups"`
	Overrides types.Dynamic `tfsdk:"overrides"`
}

var segmentConditionsUniqueIDs = uniqueIDs{
	description: "Ensures every condition id in this value is unique.",
	summary:     "Duplicate condition id",
	detail: "Each condition id must be unique across the whole value, including across different groups. Give " +
		"each condition its own seed for provider::configdirector::rule_id(...).",
}

const segmentConditionShape = "Each group is a list of attribute conditions in the API's shape ({ id, attribute, " +
	"operator, targetType, targetValues, and trait for the traits attribute }); a context is in the segment when " +
	"every condition of any one group matches. Every condition needs an \"id\": use " +
	"provider::configdirector::rule_id(\"some-stable-name\") with a distinct seed per condition. Not validated by " +
	"Terraform beyond structure and id uniqueness - passed through as-is and validated by the API. Write-only: " +
	"the API returns every condition with its kind filled in, so, like configdirector_config_targeting_rules' " +
	"rules, this is never reconciled against a subsequent read and external changes won't show up as drift."

func (r *SegmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (r *SegmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A segment: a named, reusable test of a context, defined once per project and used by the " +
			"targeting rules of any config in the project through a segment condition " +
			"({ id, kind = \"segment\", operator = \"in\" or \"not in\", segmentId }) whose segmentId is this " +
			"resource's id attribute.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The segment's id, referenced as segmentId by segment conditions in targeting rules.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project this segment belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Required: true,
				Description: "The segment's key, unique in the project. Freely renameable: targeting rules " +
					"reference the segment by id, so a rename affects nothing deployed.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 150),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The segment's display name.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 150),
				},
			},
			"kind": schema.StringAttribute{
				Computed:      true,
				Description:   "The segment kind. Only rule-based segments exist today.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"groups": schema.DynamicAttribute{
				Required: true,
				Description: "The project definition's condition groups, as a list of groups (at least one). " +
					segmentConditionShape,
				Validators: []validator.Dynamic{segmentConditionsUniqueIDs},
			},
			"overrides": schema.DynamicAttribute{
				Optional: true,
				Description: "Environment overrides, as an object keyed by environment id whose values are complete " +
					"lists of condition groups in the same shape as \"groups\": an overridden environment uses its " +
					"own groups instead of the project definition. This resource owns every override of the " +
					"segment: omitting the attribute, or removing an environment from it, removes that override. " +
					segmentConditionShape,
				Validators: []validator.Dynamic{segmentConditionsUniqueIDs},
			},
		},
	}
}

func (r *SegmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func segmentToModel(s *client.Segment, m *SegmentModel) {
	m.Id = stringValue(s.ID)
	m.ProjectId = stringValue(s.ProjectID)
	m.Key = stringValue(s.Key)
	m.Name = stringValue(s.Name)
	m.Kind = stringValue(s.Kind)
}

func (r *SegmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SegmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, overrides, diags := segmentDefinitionFromPlan(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segment, err := r.client.CreateSegment(ctx, plan.ProjectId.ValueString(), client.CreateSegmentRequest{
		Key:       plan.Key.ValueString(),
		Name:      plan.Name.ValueString(),
		Groups:    groups,
		Overrides: overrides,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Segment", err.Error())
		return
	}

	segmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SegmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SegmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	segment, err := r.client.GetSegment(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Segment", err.Error())
		return
	}

	segmentToModel(segment, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SegmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SegmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, overrides, diags := segmentDefinitionFromPlan(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segment, err := r.client.UpdateSegment(ctx, plan.ProjectId.ValueString(), state.Key.ValueString(), client.UpdateSegmentRequest{
		ID:        state.Id.ValueString(),
		Key:       plan.Key.ValueString(),
		Name:      plan.Name.ValueString(),
		Groups:    groups,
		Overrides: overrides,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Segment", err.Error())
		return
	}

	segmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SegmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SegmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSegment(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Segment", err.Error())
	}
}

func (r *SegmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id_or_slug/segment_key. Got: %q", req.ID),
		)
		return
	}

	projectID, err := resolveProjectID(ctx, r.client, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Project", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[1])...)
}

func segmentDefinitionFromPlan(plan SegmentModel) (groups any, overrides map[string]any, diags diag.Diagnostics) {
	groups, err := jsonFromDynamic(plan.Groups)
	if err != nil {
		diags.AddAttributeError(path.Root("groups"), "Invalid groups", err.Error())
	}
	overrides, err = overridesFromDynamic(plan.Overrides)
	if err != nil {
		diags.AddAttributeError(path.Root("overrides"), "Invalid overrides", err.Error())
	}
	return groups, overrides, diags
}

func overridesFromDynamic(d types.Dynamic) (map[string]any, error) {
	raw, err := jsonFromDynamic(d)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return map[string]any{}, nil
	}
	overrides, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("overrides must be an object keyed by environment id, got %T", raw)
	}
	return overrides, nil
}
