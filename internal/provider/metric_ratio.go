package provider

import (
	"context"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

var ratioSideSumsAValueField = map[string]bool{
	"events": false,
	"value":  true,
}

type metricRatioSideModel struct {
	EventName  types.String `tfsdk:"event_name"`
	Adds       types.String `tfsdk:"adds"`
	Filter     types.Set    `tfsdk:"filter"`
	ValueField types.String `tfsdk:"value_field"`
}

type metricRatioModel struct {
	Numerator   types.Object `tfsdk:"numerator"`
	Denominator types.Object `tfsdk:"denominator"`
}

var metricRatioSideType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"event_name":  types.StringType,
	"adds":        types.StringType,
	"filter":      types.SetType{ElemType: metricFilterConditionType},
	"value_field": types.StringType,
}}

var metricRatioType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"numerator":   metricRatioSideType,
	"denominator": metricRatioSideType,
}}

func metricRatioSideAttribute(description, side string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: description,
		Attributes: map[string]schema.Attribute{
			"event_name": metricDefinitionEventNameAttribute("The name of the tracked event the " + side + " adds up, exactly as the application reports it, letter case included."),
			"adds": schema.StringAttribute{
				Required: true,
				Description: "What each event adds to the " + side + ": \"events\", where each event adds 1, or \"value\", " +
					"where each event adds its value, or the number in its value_field.",
				Validators: []validator.String{stringvalidator.OneOf(slices.Sorted(maps.Keys(ratioSideSumsAValueField))...)},
			},
			"filter": metricFilterAttribute("The conditions an event's details must all match to count for the " + side +
				", unordered: 1 to 5, each written as a condition of the metric's filter. Leave filter out to count every " +
				"event with the event name."),
			"value_field": schema.StringAttribute{
				Optional: true,
				Description: "The detail field whose number each event adds to the " + side + " in place of its value, " +
					"as a JSON pointer into the event's details (RFC 6901), such as /order/total, up to 500 characters. " +
					"Only a side whose adds is \"value\" takes one. An event whose field is missing or not a number adds nothing.",
				Validators: append(detailFieldValidators(), valueFieldOnlyOnASideThatAddsValue{}),
			},
		},
	}
}

func metricRatioAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Description: "Makes the metric a ratio: one total divided by another, such as revenue per order. Each total " +
			"adds up the events or the value of one event name over a variation's exposed contexts, and the two totals " +
			"are divided once. A ratio metric has no event_name, filter, value_field or kinds of its own, and reports " +
			"the \"ratio\" kind. A metric keeps its ratio, so changing it replaces the metric, which detaches the old " +
			"one from its experiments and fails while one of them has an open run: stop the run first.",
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		Attributes: map[string]schema.Attribute{
			"numerator":   metricRatioSideAttribute("The total divided by the denominator.", "numerator"),
			"denominator": metricRatioSideAttribute("The total the numerator is divided by.", "denominator"),
		},
	}
}

type valueFieldOnlyOnASideThatAddsValue struct{}

func (v valueFieldOnlyOnASideThatAddsValue) Description(ctx context.Context) string {
	return "only a side whose adds is \"value\" takes a value_field"
}

func (v valueFieldOnlyOnASideThatAddsValue) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v valueFieldOnlyOnASideThatAddsValue) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var adds types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, req.Path.ParentPath().AtName("adds"), &adds)...)
	if resp.Diagnostics.HasError() || !isDeclared(adds) {
		return
	}

	if sumsAValueField, addsIsKnown := ratioSideSumsAValueField[adds.ValueString()]; addsIsKnown && !sumsAValueField {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid value field",
			"A side that adds events cannot have a value field. Set adds to value to sum the field.",
		)
	}
}

func metricRatioSideForCreate(ctx context.Context, sideObject types.Object) (client.MetricRatioSide, diag.Diagnostics) {
	var side metricRatioSideModel
	diagnostics := sideObject.As(ctx, &side, basetypes.ObjectAsOptions{})
	filter, filterDiagnostics := metricFilterConditionsIn(ctx, side.Filter)
	diagnostics.Append(filterDiagnostics...)
	return client.MetricRatioSide{
		EventName:  side.EventName.ValueString(),
		Adds:       side.Adds.ValueString(),
		Filter:     filter,
		ValueField: stringPtrFromValue(side.ValueField),
	}, diagnostics
}

func metricRatioForCreate(ctx context.Context, ratioObject types.Object) (*client.MetricRatio, diag.Diagnostics) {
	if ratioObject.IsNull() {
		return nil, nil
	}

	var ratio metricRatioModel
	diagnostics := ratioObject.As(ctx, &ratio, basetypes.ObjectAsOptions{})
	numerator, numeratorDiagnostics := metricRatioSideForCreate(ctx, ratio.Numerator)
	diagnostics.Append(numeratorDiagnostics...)
	denominator, denominatorDiagnostics := metricRatioSideForCreate(ctx, ratio.Denominator)
	diagnostics.Append(denominatorDiagnostics...)
	return &client.MetricRatio{Numerator: numerator, Denominator: denominator}, diagnostics
}

func metricRatioSideObjectValue(ctx context.Context, side client.MetricRatioSide) (types.Object, diag.Diagnostics) {
	filter, diagnostics := metricFilterSetValue(ctx, side.Filter)
	sideObject, sideDiagnostics := types.ObjectValueFrom(ctx, metricRatioSideType.AttrTypes, metricRatioSideModel{
		EventName:  types.StringValue(side.EventName),
		Adds:       types.StringValue(side.Adds),
		Filter:     filter,
		ValueField: stringPtrValue(side.ValueField),
	})
	diagnostics.Append(sideDiagnostics...)
	return sideObject, diagnostics
}

func metricRatioObjectValue(ctx context.Context, ratio *client.MetricRatio) (types.Object, diag.Diagnostics) {
	if ratio == nil {
		return types.ObjectNull(metricRatioType.AttrTypes), nil
	}

	numerator, diagnostics := metricRatioSideObjectValue(ctx, ratio.Numerator)
	denominator, denominatorDiagnostics := metricRatioSideObjectValue(ctx, ratio.Denominator)
	diagnostics.Append(denominatorDiagnostics...)
	ratioObject, ratioDiagnostics := types.ObjectValueFrom(ctx, metricRatioType.AttrTypes, metricRatioModel{
		Numerator:   numerator,
		Denominator: denominator,
	})
	diagnostics.Append(ratioDiagnostics...)
	return ratioObject, diagnostics
}
