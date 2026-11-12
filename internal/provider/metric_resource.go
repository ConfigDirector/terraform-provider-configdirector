package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                     = &metricResource{}
	_ resource.ResourceWithImportState      = &metricResource{}
	_ resource.ResourceWithConfigValidators = &metricResource{}
)

var (
	metricDirections = []string{"higher-is-better", "lower-is-better"}
	metricKinds      = []string{"conversion", "count-per-context", "value-per-context"}
)

func newMetricResource() resource.Resource {
	return &metricResource{}
}

type metricResource struct {
	client *client.Client
}

type metricModel struct {
	Id          types.String `tfsdk:"id"`
	ProjectId   types.String `tfsdk:"project_id"`
	EventName   types.String `tfsdk:"event_name"`
	Filter      types.Set    `tfsdk:"filter"`
	ValueField  types.String `tfsdk:"value_field"`
	Description types.String `tfsdk:"description"`
	Direction   types.String `tfsdk:"direction"`
	Kinds       types.Set    `tfsdk:"kinds"`
}

func (r *metricResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metric"
}

func (r *metricResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A metric: what an experiment measures, defined in a project for one tracked event name, and " +
			"attached to any of the project's experiments through the metrics of configdirector_config, whose " +
			"metric_id is this resource's id attribute. A metric can count only the events whose details match a " +
			"filter, and sum a detail field in place of the event's value, so a project can have several metrics on " +
			"one event name, each with its own filter or value field. A metric with a filter or a value field counts " +
			"the events reported from the moment it is attached to an experiment, never earlier ones. Destroying a " +
			"metric detaches it from its experiments, and fails while an experiment it is attached to has an open " +
			"run. Import with project_id_or_slug/metric_id, or with project_id_or_slug/event_name while the event " +
			"name has one metric: only the first slash separates the project from the rest, which is read as a " +
			"metric id when it is a UUID and otherwise as an event name, taken literally. The API token needs " +
			"metrics:read, metrics:create, metrics:update and metrics:delete.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The metric's id, referenced as metric_id in the metrics of configdirector_config.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project this metric belongs to. Changing the project replaces the metric.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"event_name": schema.StringAttribute{
				Required: true,
				Description: "The name of the tracked event the metric measures, exactly as the application reports it, " +
					"letter case included. A metric keeps its event name, so changing it replaces the metric, which " +
					"detaches the old one from its experiments and fails while one of them has an open run.",
				Validators:    []validator.String{stringvalidator.UTF8LengthBetween(1, 150)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"filter": schema.SetNestedAttribute{
				Optional: true,
				Description: "The conditions an event's details must all match for the event to count, unordered: 1 to 5. " +
					"Leave filter out to count every event with the event name. A metric keeps its filter, so changing, " +
					"adding or removing a condition replaces the metric, which detaches the old one from its experiments " +
					"and fails while one of them has an open run: stop the run first.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
				Validators:    []validator.Set{metricFilterHasConditions{}, setvalidator.SizeAtMost(metricFilterConditionsLimit)},
				NestedObject: schema.NestedAttributeObject{
					Validators: []validator.Object{metricFilterConditionFitsItsTargetType{}},
					Attributes: map[string]schema.Attribute{
						"field": schema.StringAttribute{
							Required: true,
							Description: "The detail field the condition reads, as a JSON pointer into the event's details " +
								"(RFC 6901), such as /plan or /address/country, up to 500 characters. An event without the " +
								"field matches only the \"is NOT one of\" and \"!=\" operators.",
							Validators: []validator.String{
								stringvalidator.UTF8LengthAtMost(detailFieldMaximumCharacters),
								detailFieldIsAJSONPointer{},
							},
						},
						"target_type": schema.StringAttribute{
							Required:    true,
							Description: "How the condition compares the field: \"text\", \"number\" or \"boolean\".",
							Validators:  []validator.String{stringvalidator.OneOf(slices.Sorted(maps.Keys(metricFilterTargetTypes))...)},
						},
						"operator": schema.StringAttribute{
							Required: true,
							Description: "For text, \"is one of\" or \"is NOT one of\"; for number, \"=\", \"!=\", " +
								"\">\", \">=\", \"<\" or \"<=\"; for boolean, \"equals\".",
						},
						"target_values": schema.SetAttribute{
							Required:    true,
							ElementType: types.StringType,
							Description: "What the field is compared with: for text, 1 to 20 values of up to 500 characters " +
								"each; for number, one number, such as \"2\" or \"9.99\"; for boolean, \"true\" or \"false\".",
							Validators: []validator.Set{
								setvalidator.ValueStringsAre(
									targetValueIsNotBlank{},
									stringvalidator.UTF8LengthAtMost(targetValueMaximumCharacters),
								),
							},
						},
					},
				},
			},
			"value_field": schema.StringAttribute{
				Optional: true,
				Description: "The detail field whose number the metric sums in place of the event's value, as a JSON " +
					"pointer into the event's details (RFC 6901), such as /items, up to 500 characters. An event whose " +
					"field is missing or not a number counts as an event and adds nothing to the sum. A metric with a " +
					"value field reports \"value-per-context\", so kinds must include it. A metric keeps its value field, " +
					"so changing, adding or removing it replaces the metric, which detaches the old one from its " +
					"experiments and fails while one of them has an open run: stop the run first.",
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtMost(detailFieldMaximumCharacters),
					detailFieldIsAJSONPointer{},
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "What the metric measures, up to 1,000 characters. Leaving it out removes the description.",
				Validators:  []validator.String{stringvalidator.UTF8LengthAtMost(1000)},
			},
			"direction": schema.StringAttribute{
				Required:    true,
				Description: "Whether a higher or a lower number is better: \"higher-is-better\" or \"lower-is-better\".",
				Validators:  []validator.String{stringvalidator.OneOf(metricDirections...)},
			},
			"kinds": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "What the metric reports, at least one of: \"conversion\" (the share of exposed contexts " +
					"that reported the event at least once), \"count-per-context\" (the events each exposed context " +
					"reported, averaged) and \"value-per-context\" (the sum of the event's value, or of its value_field, per " +
					"exposed context, averaged). The primary metric of an experiment decides on one of these kinds.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(metricKinds...)),
				},
			},
		},
	}
}

func (r *metricResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{valueFieldReportedAsValuePerContext{}}
}

func (r *metricResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, isClient := req.ProviderData.(*client.Client)
	if !isClient {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = api
}

func metricToModel(ctx context.Context, metric *client.Metric, model *metricModel) diag.Diagnostics {
	model.Id = stringValue(metric.ID)
	model.ProjectId = stringValue(metric.ProjectID)
	model.EventName = stringValue(metric.EventName)
	model.ValueField = stringPtrValue(metric.ValueField)
	model.Description = declaredOrSaved(model.Description, savedText(metric.Description))
	model.Direction = stringValue(metric.Direction)
	filter, diagnostics := metricFilterSetValue(ctx, metric.Filter)
	model.Filter = filter
	kinds, kindsDiagnostics := types.SetValueFrom(ctx, types.StringType, metric.Kinds)
	model.Kinds = kinds
	diagnostics.Append(kindsDiagnostics...)
	return diagnostics
}

func metricKindsFromPlan(ctx context.Context, plan metricModel) ([]string, diag.Diagnostics) {
	var kinds []string
	diagnostics := plan.Kinds.ElementsAs(ctx, &kinds, false)
	return kinds, diagnostics
}

func (r *metricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter, diagnostics := metricFilterForCreate(ctx, plan.Filter)
	resp.Diagnostics.Append(diagnostics...)
	kinds, diagnostics := metricKindsFromPlan(ctx, plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.CreateMetric(ctx, plan.ProjectId.ValueString(), client.CreateMetricRequest{
		EventName:   plan.EventName.ValueString(),
		Filter:      filter,
		ValueField:  stringPtrFromValue(plan.ValueField),
		Description: stringPtrFromValue(plan.Description),
		Direction:   plan.Direction.ValueString(),
		Kinds:       kinds,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Metric", err.Error())
		return
	}

	resp.Diagnostics.Append(metricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *metricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.GetMetric(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Metric", err.Error())
		return
	}

	resp.Diagnostics.Append(metricToModel(ctx, metric, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *metricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	kinds, diagnostics := metricKindsFromPlan(ctx, plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.UpdateMetric(ctx, state.ProjectId.ValueString(), state.Id.ValueString(), client.UpdateMetricRequest{
		Description: stringPtrFromValue(plan.Description),
		Direction:   plan.Direction.ValueString(),
		Kinds:       kinds,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Metric", err.Error())
		return
	}

	resp.Diagnostics.Append(metricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *metricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMetric(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Metric", err.Error())
	}
}

func (r *metricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectIDOrSlug, reference, found := strings.Cut(req.ID, "/")
	if !found || projectIDOrSlug == "" || reference == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id_or_slug/metric_id or project_id_or_slug/event_name. Got: %q", req.ID),
		)
		return
	}

	projectID, err := resolveProjectID(ctx, r.client, projectIDOrSlug)
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Project", err.Error())
		return
	}

	metric, diagnostics := metricSelectionOf(reference).findMetric(ctx, r.client, projectIDOrSlug, projectID)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state metricModel
	resp.Diagnostics.Append(metricToModel(ctx, metric, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
