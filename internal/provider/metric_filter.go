package provider

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const (
	metricFilterConditionsLimit    = 5
	detailFieldMaximumCharacters   = 500
	textConditionTargetValuesLimit = 20
	targetValueMaximumCharacters   = 500
)

type metricFilterConditionModel struct {
	Field        types.String `tfsdk:"field"`
	TargetType   types.String `tfsdk:"target_type"`
	Operator     types.String `tfsdk:"operator"`
	TargetValues types.Set    `tfsdk:"target_values"`
}

var metricFilterConditionType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"field":         types.StringType,
	"target_type":   types.StringType,
	"operator":      types.StringType,
	"target_values": types.SetType{ElemType: types.StringType},
}}

var decimalNumberPattern = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)

func isDecimalNumber(text string) bool {
	if !decimalNumberPattern.MatchString(text) {
		return false
	}
	number, _ := strconv.ParseFloat(text, 64)
	return !math.IsInf(number, 0)
}

type metricFilterTargetType struct {
	operators          []string
	refuseTargetValues func(targetValues []string) (refusal string, refused bool)
	comparisonWords    func(condition client.MetricFilterCondition) string
}

func comparisonWithTargetValueList(condition client.MetricFilterCondition) string {
	return condition.Operator + " [" + strings.Join(condition.TargetValues, ", ") + "]"
}

func firstTargetValue(condition client.MetricFilterCondition) string {
	if len(condition.TargetValues) == 0 {
		return ""
	}
	return condition.TargetValues[0]
}

var metricFilterTargetTypes = map[string]metricFilterTargetType{
	"text": {
		operators: []string{"is one of", "is NOT one of"},
		refuseTargetValues: func(targetValues []string) (string, bool) {
			switch {
			case len(targetValues) == 0:
				return "A text condition needs at least one target value", true
			case len(targetValues) > textConditionTargetValuesLimit:
				return fmt.Sprintf("A text condition cannot have more than %d target values", textConditionTargetValuesLimit), true
			default:
				return "", false
			}
		},
		comparisonWords: comparisonWithTargetValueList,
	},
	"number": {
		operators: []string{"=", "!=", ">", ">=", "<", "<="},
		refuseTargetValues: func(targetValues []string) (string, bool) {
			switch {
			case len(targetValues) != 1:
				return "A number condition needs exactly one target value", true
			case !isDecimalNumber(targetValues[0]):
				return "The target value of a number condition must be a number", true
			default:
				return "", false
			}
		},
		comparisonWords: func(condition client.MetricFilterCondition) string {
			return condition.Operator + " " + firstTargetValue(condition)
		},
	},
	"boolean": {
		operators: []string{"equals"},
		refuseTargetValues: func(targetValues []string) (string, bool) {
			switch {
			case len(targetValues) != 1:
				return "A boolean condition needs exactly one target value, true or false", true
			case targetValues[0] != "true" && targetValues[0] != "false":
				return "The target value of a boolean condition must be true or false", true
			default:
				return "", false
			}
		},
		comparisonWords: func(condition client.MetricFilterCondition) string {
			return "is " + firstTargetValue(condition)
		},
	},
}

func isJSONPointer(text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] != '~' {
			continue
		}
		if index+1 == len(text) || (text[index+1] != '0' && text[index+1] != '1') {
			return false
		}
		index++
	}
	return true
}

type detailFieldIsAJSONPointer struct{}

func (v detailFieldIsAJSONPointer) Description(ctx context.Context) string {
	return "A detail field must be a JSON pointer (RFC 6901), such as /plan"
}

func (v detailFieldIsAJSONPointer) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v detailFieldIsAJSONPointer) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || isJSONPointer(req.ConfigValue.ValueString()) {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid detail field", v.Description(ctx))
}

type targetValueIsNotBlank struct{}

func (v targetValueIsNotBlank) Description(ctx context.Context) string {
	return "A target value cannot be blank"
}

func (v targetValueIsNotBlank) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v targetValueIsNotBlank) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || trimJavaScriptWhitespace(req.ConfigValue.ValueString()) != "" {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid filter target value", v.Description(ctx))
}

func isFullyKnown(set types.Set) bool {
	return !set.IsUnknown() && !slices.ContainsFunc(set.Elements(), attr.Value.IsUnknown)
}

type valueFieldReportedAsValuePerContext struct{}

func (v valueFieldReportedAsValuePerContext) Description(ctx context.Context) string {
	return "a metric with a value_field must have \"value-per-context\" among its kinds"
}

func (v valueFieldReportedAsValuePerContext) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v valueFieldReportedAsValuePerContext) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var valueField types.String
	var kinds types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value_field"), &valueField)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("kinds"), &kinds)...)
	if resp.Diagnostics.HasError() || valueField.IsNull() || valueField.IsUnknown() || kinds.IsNull() || !isFullyKnown(kinds) {
		return
	}

	if !slices.Contains(kinds.Elements(), attr.Value(types.StringValue("value-per-context"))) {
		resp.Diagnostics.AddAttributeError(path.Root("kinds"), "Invalid kinds", "A metric with a value field needs value-per-context among its kinds")
	}
}

type metricFilterHasConditions struct{}

func (v metricFilterHasConditions) Description(ctx context.Context) string {
	return "a filter needs at least one condition"
}

func (v metricFilterHasConditions) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v metricFilterHasConditions) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || len(req.ConfigValue.Elements()) > 0 {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid filter", "A filter needs at least one condition. Leave filter out for a metric without a filter.")
}

type metricFilterConditionFitsItsTargetType struct{}

func (v metricFilterConditionFitsItsTargetType) Description(ctx context.Context) string {
	return "the operator and target values must be ones the condition's target type takes"
}

func (v metricFilterConditionFitsItsTargetType) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v metricFilterConditionFitsItsTargetType) ValidateObject(ctx context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var condition metricFilterConditionModel
	resp.Diagnostics.Append(req.ConfigValue.As(ctx, &condition, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() || condition.TargetType.IsUnknown() {
		return
	}
	targetTypeName := condition.TargetType.ValueString()
	targetType, isTargetType := metricFilterTargetTypes[targetTypeName]
	if !isTargetType {
		return
	}

	if !condition.Operator.IsUnknown() && !slices.Contains(targetType.operators, condition.Operator.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			req.Path.AtName("operator"),
			"Invalid filter operator",
			fmt.Sprintf("The operator of a %s condition must be one of: %s", targetTypeName, strings.Join(targetType.operators, ", ")),
		)
	}

	if !isFullyKnown(condition.TargetValues) {
		return
	}
	var targetValues []string
	resp.Diagnostics.Append(condition.TargetValues.ElementsAs(ctx, &targetValues, false)...)
	if refusal, refused := targetType.refuseTargetValues(targetValues); refused {
		resp.Diagnostics.AddAttributeError(req.Path.AtName("target_values"), "Invalid filter target values", refusal)
	}
}

func metricFilterForCreate(ctx context.Context, filter types.Set) ([]client.MetricFilterCondition, diag.Diagnostics) {
	if filter.IsNull() {
		return nil, nil
	}

	var declared []metricFilterConditionModel
	diagnostics := filter.ElementsAs(ctx, &declared, false)
	conditions := make([]client.MetricFilterCondition, len(declared))
	for i, condition := range declared {
		var targetValues []string
		diagnostics.Append(condition.TargetValues.ElementsAs(ctx, &targetValues, false)...)
		conditions[i] = client.MetricFilterCondition{
			Field:        condition.Field.ValueString(),
			TargetType:   condition.TargetType.ValueString(),
			Operator:     condition.Operator.ValueString(),
			TargetValues: targetValues,
		}
	}
	return conditions, diagnostics
}

func metricFilterSetValue(ctx context.Context, filter []client.MetricFilterCondition) (types.Set, diag.Diagnostics) {
	if filter == nil {
		return types.SetNull(metricFilterConditionType), nil
	}

	var diagnostics diag.Diagnostics
	models := make([]metricFilterConditionModel, len(filter))
	for i, condition := range filter {
		targetValues, targetValuesDiagnostics := types.SetValueFrom(ctx, types.StringType, condition.TargetValues)
		diagnostics.Append(targetValuesDiagnostics...)
		models[i] = metricFilterConditionModel{
			Field:        stringValue(condition.Field),
			TargetType:   stringValue(condition.TargetType),
			Operator:     stringValue(condition.Operator),
			TargetValues: targetValues,
		}
	}
	filterValue, filterDiagnostics := types.SetValueFrom(ctx, metricFilterConditionType, models)
	diagnostics.Append(filterDiagnostics...)
	return filterValue, diagnostics
}
