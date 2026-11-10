package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var metricRoles = []string{"primary", "secondary", "guardrail"}

type configMetricModel struct {
	MetricId    types.String `tfsdk:"metric_id"`
	Role        types.String `tfsdk:"role"`
	PrimaryKind types.String `tfsdk:"primary_kind"`
}

var configMetricAttrTypes = map[string]attr.Type{
	"metric_id":    types.StringType,
	"role":         types.StringType,
	"primary_kind": types.StringType,
}

func configMetricsForWrite(ctx context.Context, configuration tfsdk.Config, plannedMetrics types.Set) (*[]client.MetricAttachment, diag.Diagnostics) {
	var configuredMetrics types.Set
	diagnostics := configuration.GetAttribute(ctx, path.Root("metrics"), &configuredMetrics)
	if diagnostics.HasError() || configuredMetrics.IsNull() {
		return nil, diagnostics
	}

	var declared []configMetricModel
	diagnostics.Append(plannedMetrics.ElementsAs(ctx, &declared, false)...)
	attachments := make([]client.MetricAttachment, len(declared))
	for i, metric := range declared {
		attachments[i] = client.MetricAttachment{
			MetricID:    metric.MetricId.ValueString(),
			Role:        metric.Role.ValueString(),
			PrimaryKind: stringPtrFromValue(metric.PrimaryKind),
		}
	}
	return &attachments, diagnostics
}

func configMetricsSetValue(ctx context.Context, attachments []client.MetricAttachment) (types.Set, diag.Diagnostics) {
	models := make([]configMetricModel, len(attachments))
	for i, attachment := range attachments {
		models[i] = configMetricModel{
			MetricId:    stringValue(attachment.MetricID),
			Role:        stringValue(attachment.Role),
			PrimaryKind: stringPtrValue(attachment.PrimaryKind),
		}
	}
	return types.SetValueFrom(ctx, types.ObjectType{AttrTypes: configMetricAttrTypes}, models)
}

type requiredInEachMetric struct {
	setting string
}

func (v requiredInEachMetric) Description(ctx context.Context) string {
	return "Each metric needs " + v.setting + "."
}

func (v requiredInEachMetric) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v requiredInEachMetric) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		resp.Diagnostics.AddAttributeError(req.Path, "Missing metric setting", v.Description(ctx))
	}
}
