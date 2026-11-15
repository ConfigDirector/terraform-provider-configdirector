package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const (
	fewestStepsOfAFunnel = 2
	mostStepsOfAFunnel   = 5
)

type metricFunnelStepModel struct {
	EventName types.String `tfsdk:"event_name"`
	Filter    types.Set    `tfsdk:"filter"`
}

type metricFunnelModel struct {
	Steps types.List `tfsdk:"steps"`
}

var metricFunnelStepType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"event_name": types.StringType,
	"filter":     types.SetType{ElemType: metricFilterConditionType},
}}

var metricFunnelType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"steps": types.ListType{ElemType: metricFunnelStepType},
}}

func metricFunnelAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Description: "Makes the metric a funnel: the share of exposed contexts that reached the last of its steps, " +
			"in order, with how many contexts reached each step. A funnel metric has no event_name, filter, " +
			"value_field or kinds of its own, and reports the \"funnel\" kind. A metric keeps its steps and their " +
			"order, so changing or reordering them replaces the metric, which detaches the old one from its " +
			"experiments and fails while one of them has an open run: stop the run first.",
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		Attributes: map[string]schema.Attribute{
			"steps": schema.ListNestedAttribute{
				Required:   true,
				Validators: []validator.List{funnelHasTwoToFiveSteps{}, funnelStepRepeatsNoEarlierStep{}},
				Description: "The 2 to 5 steps, in the order a context reaches them. A context reaches the first step " +
					"with an event that matches it, and each later step with an event that matches it and happened at " +
					"or after the moment it reached the step before, with no time limit but the run's end. Two steps " +
					"can name the same event when their filters differ.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"event_name": metricDefinitionEventNameAttribute("The name of the tracked event that reaches the step, exactly as the application reports it, letter case included."),
						"filter": metricFilterAttribute("The conditions an event's details must all match to reach the " +
							"step, unordered: 1 to 5, each written as a condition of the metric's filter. Leave filter out " +
							"for every event with the event name to reach it."),
					},
				},
			},
		},
	}
}

type funnelHasTwoToFiveSteps struct{}

func (v funnelHasTwoToFiveSteps) Description(ctx context.Context) string {
	return fmt.Sprintf("a funnel has %d to %d steps", fewestStepsOfAFunnel, mostStepsOfAFunnel)
}

func (v funnelHasTwoToFiveSteps) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v funnelHasTwoToFiveSteps) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	stepCount := len(req.ConfigValue.Elements())
	if stepCount < fewestStepsOfAFunnel || stepCount > mostStepsOfAFunnel {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid funnel steps",
			fmt.Sprintf("A funnel has %d to %d steps.", fewestStepsOfAFunnel, mostStepsOfAFunnel),
		)
	}
}

func isKnownThroughout(ctx context.Context, value attr.Value) bool {
	terraformValue, err := value.ToTerraformValue(ctx)
	return err == nil && terraformValue.IsFullyKnown()
}

func funnelStepAsCompared(ctx context.Context, stepValue attr.Value) (string, bool) {
	stepObject, isObject := stepValue.(types.Object)
	if !isObject || !isKnownThroughout(ctx, stepObject) {
		return "", false
	}
	var step metricFunnelStepModel
	if diagnostics := stepObject.As(ctx, &step, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
		return "", false
	}
	filter, diagnostics := metricFilterConditionsIn(ctx, step.Filter)
	if diagnostics.HasError() {
		return "", false
	}

	var filterAsCompared any
	if filter != nil {
		filterAsCompared = metricFilterAsCompared(filter)
	}
	stepAsCompared, _ := json.Marshal([]any{step.EventName.ValueString(), filterAsCompared})
	return string(stepAsCompared), true
}

type funnelStepRepeatsNoEarlierStep struct{}

func (v funnelStepRepeatsNoEarlierStep) Description(ctx context.Context) string {
	return "no step of a funnel has the same event name and filter as an earlier step"
}

func (v funnelStepRepeatsNoEarlierStep) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v funnelStepRepeatsNoEarlierStep) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	firstIndexOfEachStep := map[string]int{}
	for index, stepValue := range req.ConfigValue.Elements() {
		stepAsCompared, known := funnelStepAsCompared(ctx, stepValue)
		if !known {
			continue
		}
		indexOfTheStepItRepeats, repeats := firstIndexOfEachStep[stepAsCompared]
		if !repeats {
			firstIndexOfEachStep[stepAsCompared] = index
			continue
		}
		resp.Diagnostics.AddAttributeError(
			req.Path.AtListIndex(index),
			"Repeated funnel step",
			fmt.Sprintf("Step %d repeats step %d: the same event name and filter. Change one of them or remove the step.", index+1, indexOfTheStepItRepeats+1),
		)
	}
}

func metricFunnelForCreate(ctx context.Context, funnelObject types.Object) (*client.MetricFunnel, diag.Diagnostics) {
	if funnelObject.IsNull() {
		return nil, nil
	}

	var funnel metricFunnelModel
	diagnostics := funnelObject.As(ctx, &funnel, basetypes.ObjectAsOptions{})
	var steps []metricFunnelStepModel
	diagnostics.Append(funnel.Steps.ElementsAs(ctx, &steps, false)...)
	stepsForCreate := make([]client.MetricFunnelStep, len(steps))
	for i, step := range steps {
		filter, filterDiagnostics := metricFilterConditionsIn(ctx, step.Filter)
		diagnostics.Append(filterDiagnostics...)
		stepsForCreate[i] = client.MetricFunnelStep{EventName: step.EventName.ValueString(), Filter: filter}
	}
	return &client.MetricFunnel{Steps: stepsForCreate}, diagnostics
}

func metricFunnelObjectValue(ctx context.Context, funnel *client.MetricFunnel) (types.Object, diag.Diagnostics) {
	if funnel == nil {
		return types.ObjectNull(metricFunnelType.AttrTypes), nil
	}

	var diagnostics diag.Diagnostics
	steps := make([]metricFunnelStepModel, len(funnel.Steps))
	for i, step := range funnel.Steps {
		filter, filterDiagnostics := metricFilterSetValue(ctx, step.Filter)
		diagnostics.Append(filterDiagnostics...)
		steps[i] = metricFunnelStepModel{EventName: types.StringValue(step.EventName), Filter: filter}
	}
	stepsValue, stepsDiagnostics := types.ListValueFrom(ctx, metricFunnelStepType, steps)
	diagnostics.Append(stepsDiagnostics...)
	funnelObject, funnelDiagnostics := types.ObjectValueFrom(ctx, metricFunnelType.AttrTypes, metricFunnelModel{Steps: stepsValue})
	diagnostics.Append(funnelDiagnostics...)
	return funnelObject, diagnostics
}
