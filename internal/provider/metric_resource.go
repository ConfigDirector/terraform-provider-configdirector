package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &metricResource{}
	_ resource.ResourceWithImportState = &metricResource{}
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
	Description types.String `tfsdk:"description"`
	Direction   types.String `tfsdk:"direction"`
	Kinds       types.Set    `tfsdk:"kinds"`
}

func (r *metricResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metric"
}

func (r *metricResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A metric: what an experiment measures, defined once per project for one tracked event name, and " +
			"attached to any of the project's experiments through the metrics of configdirector_config, whose " +
			"metric_id is this resource's id attribute. Destroying a metric detaches it from its experiments, and " +
			"fails while an experiment it is attached to has an open run. Import with " +
			"project_id_or_slug/event_name: only the first slash separates the project from the event name, which is " +
			"taken literally. The API token needs metrics:read, metrics:create, metrics:update and metrics:delete.",
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
					"letter case included. A project has one metric per event name. A metric keeps its event name, so " +
					"changing it replaces the metric, which detaches the old one from its experiments and fails while one " +
					"of them has an open run.",
				Validators:    []validator.String{stringvalidator.UTF8LengthBetween(1, 150)},
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
					"reported, averaged) and \"value-per-context\" (the sum of the event's value per exposed context, " +
					"averaged). The primary metric of an experiment decides on one of these kinds.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(metricKinds...)),
				},
			},
		},
	}
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
	model.Description = declaredOrSaved(model.Description, savedText(metric.Description))
	model.Direction = stringValue(metric.Direction)
	kinds, diagnostics := types.SetValueFrom(ctx, types.StringType, metric.Kinds)
	model.Kinds = kinds
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

	kinds, diagnostics := metricKindsFromPlan(ctx, plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.CreateMetric(ctx, plan.ProjectId.ValueString(), client.CreateMetricRequest{
		EventName:   plan.EventName.ValueString(),
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
	project, eventName, found := strings.Cut(req.ID, "/")
	if !found || project == "" || eventName == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id_or_slug/event_name. Got: %q", req.ID),
		)
		return
	}

	projectID, err := resolveProjectID(ctx, r.client, project)
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Project", err.Error())
		return
	}

	metrics, err := r.client.ListMetricsWithEventName(ctx, projectID, eventName)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Metric", err.Error())
		return
	}
	if len(metrics) == 0 {
		resp.Diagnostics.AddError(
			"Metric Not Found",
			fmt.Sprintf("Project %q has no metric for the event name %q. The event name is matched exactly, letter case included.", project, eventName),
		)
		return
	}

	var state metricModel
	resp.Diagnostics.Append(metricToModel(ctx, &metrics[0], &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
