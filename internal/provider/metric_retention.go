package provider

import (
	"context"
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
	earliestDayOfRetention = 1
	latestDayOfRetention   = 90
)

type metricRetentionModel struct {
	EventName types.String `tfsdk:"event_name"`
	Filter    types.Set    `tfsdk:"filter"`
	FirstDay  types.Int64  `tfsdk:"first_day"`
	LastDay   types.Int64  `tfsdk:"last_day"`
}

var metricRetentionType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"event_name": types.StringType,
	"filter":     types.SetType{ElemType: metricFilterConditionType},
	"first_day":  types.Int64Type,
	"last_day":   types.Int64Type,
}}

func metricRetentionAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Description: "Makes the metric a retention: the share of contexts that reported an event between a first and " +
			"a last day after their exposure, such as days 7 to 13, of the contexts whose last day has passed. A day " +
			"is 24 hours counted from the context's own first exposure: day 1 starts 24 hours after it. A retention metric " +
			"has no event_name, filter, value_field or kinds of its own, and reports the \"retention\" kind. A metric " +
			"keeps its retention, so changing it replaces the metric, which detaches the old one from its experiments " +
			"and fails while one of them has an open run: stop the run first.",
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		Attributes: map[string]schema.Attribute{
			"event_name": metricDefinitionEventNameAttribute("The name of the tracked event that brings a context back, exactly as the application reports it, letter case included."),
			"filter": metricFilterAttribute("The conditions an event's details must all match to bring a context back, " +
				"unordered: 1 to 5, each written as a condition of the metric's filter. Leave filter out for every event " +
				"with the event name to count."),
			"first_day": schema.Int64Attribute{
				Required:    true,
				Description: "The first day after a context's exposure on which the event counts, a whole number from 1 to 90.",
				Validators:  []validator.Int64{dayOfRetentionIsFrom1To90{day: "first"}},
			},
			"last_day": schema.Int64Attribute{
				Required: true,
				Description: "The last day after a context's exposure on which the event counts, a whole number from 1 to " +
					"90 and no earlier than first_day; the same number as first_day is one day. A context is measured " +
					"once its last day has ended.",
				Validators: []validator.Int64{dayOfRetentionIsFrom1To90{day: "last"}, lastDayNotBeforeTheFirstDay{}},
			},
		},
	}
}

type dayOfRetentionIsFrom1To90 struct {
	day string
}

func (v dayOfRetentionIsFrom1To90) Description(ctx context.Context) string {
	return fmt.Sprintf("The %s day has to be a whole number from %d to %d.", v.day, earliestDayOfRetention, latestDayOfRetention)
}

func (v dayOfRetentionIsFrom1To90) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v dayOfRetentionIsFrom1To90) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	day := req.ConfigValue.ValueInt64()
	if day < earliestDayOfRetention || day > latestDayOfRetention {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid day of retention", v.Description(ctx))
	}
}

type lastDayNotBeforeTheFirstDay struct{}

func (v lastDayNotBeforeTheFirstDay) Description(ctx context.Context) string {
	return "the last day of a retention cannot come before its first day"
}

func (v lastDayNotBeforeTheFirstDay) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lastDayNotBeforeTheFirstDay) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var firstDay types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, req.Path.ParentPath().AtName("first_day"), &firstDay)...)
	if resp.Diagnostics.HasError() || !isDeclared(firstDay) {
		return
	}

	if req.ConfigValue.ValueInt64() < firstDay.ValueInt64() {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid day of retention", "The last day cannot come before the first day.")
	}
}

func metricRetentionForCreate(ctx context.Context, retentionObject types.Object) (*client.MetricRetention, diag.Diagnostics) {
	if retentionObject.IsNull() {
		return nil, nil
	}

	var retention metricRetentionModel
	diagnostics := retentionObject.As(ctx, &retention, basetypes.ObjectAsOptions{})
	filter, filterDiagnostics := metricFilterConditionsIn(ctx, retention.Filter)
	diagnostics.Append(filterDiagnostics...)
	return &client.MetricRetention{
		EventName: retention.EventName.ValueString(),
		Filter:    filter,
		FirstDay:  retention.FirstDay.ValueInt64(),
		LastDay:   retention.LastDay.ValueInt64(),
	}, diagnostics
}

func metricRetentionObjectValue(ctx context.Context, retention *client.MetricRetention) (types.Object, diag.Diagnostics) {
	if retention == nil {
		return types.ObjectNull(metricRetentionType.AttrTypes), nil
	}

	filter, diagnostics := metricFilterSetValue(ctx, retention.Filter)
	retentionObject, retentionDiagnostics := types.ObjectValueFrom(ctx, metricRetentionType.AttrTypes, metricRetentionModel{
		EventName: types.StringValue(retention.EventName),
		Filter:    filter,
		FirstDay:  types.Int64Value(retention.FirstDay),
		LastDay:   types.Int64Value(retention.LastDay),
	})
	diagnostics.Append(retentionDiagnostics...)
	return retentionObject, diagnostics
}
