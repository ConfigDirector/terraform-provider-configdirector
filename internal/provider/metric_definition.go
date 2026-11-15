package provider

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	eventNameMaximumCharacters = 150
	invalidMetricDefinition    = "Invalid metric definition"
)

type metricDefinitionOfItsOwnKind struct {
	declarationIn                func(model metricModel) types.Object
	refusalOfAnEventNameBesideIt string
	refusalOfAFilterBesideIt     string
	refusalOfAValueFieldBesideIt string
	refusalOfKindsBesideIt       string
}

var metricDefinitionsByKind = map[string]metricDefinitionOfItsOwnKind{
	"ratio": {
		declarationIn:                func(model metricModel) types.Object { return model.Ratio },
		refusalOfAnEventNameBesideIt: "A ratio metric has no event name of its own: its numerator and its denominator each take one. Leave event_name out.",
		refusalOfAFilterBesideIt:     "A ratio metric has no filter of its own: its numerator and its denominator each take one. Leave filter out.",
		refusalOfAValueFieldBesideIt: "A ratio metric has no value field of its own: its numerator and its denominator each take one. Leave value_field out.",
		refusalOfKindsBesideIt:       "A ratio metric reports the ratio kind alone, so kinds does not apply to it. Leave kinds out.",
	},
	"funnel": {
		declarationIn:                func(model metricModel) types.Object { return model.Funnel },
		refusalOfAnEventNameBesideIt: "A funnel metric has no event name of its own: each of its steps takes one. Leave event_name out.",
		refusalOfAFilterBesideIt:     "A funnel metric has no filter of its own: each of its steps takes one. Leave filter out.",
		refusalOfAValueFieldBesideIt: "A funnel metric has no value field: it counts the contexts that reached each step. Leave value_field out.",
		refusalOfKindsBesideIt:       "A funnel metric reports the funnel kind alone, so kinds does not apply to it. Leave kinds out.",
	},
	"retention": {
		declarationIn:                func(model metricModel) types.Object { return model.Retention },
		refusalOfAnEventNameBesideIt: "A retention metric has no event name of its own: its retention takes one. Leave event_name out.",
		refusalOfAFilterBesideIt:     "A retention metric has no filter of its own: its retention takes one. Leave filter out.",
		refusalOfAValueFieldBesideIt: "A retention metric has no value field: it counts the contexts that reported the event during their days. Leave value_field out.",
		refusalOfKindsBesideIt:       "A retention metric reports the retention kind alone, so kinds does not apply to it. Leave kinds out.",
	},
}

var kindsWithADefinitionOfTheirOwn = slices.Sorted(maps.Keys(metricDefinitionsByKind))

var metricKinds = slices.Concat(oneEventMetricKinds, kindsWithADefinitionOfTheirOwn)

func kindsOfTheDefinitionsDeclaredIn(model metricModel) []string {
	return slices.DeleteFunc(slices.Clone(kindsWithADefinitionOfTheirOwn), func(kind string) bool {
		return !isDeclared(metricDefinitionsByKind[kind].declarationIn(model))
	})
}

func isADefinitionUnknownIn(model metricModel) bool {
	return slices.ContainsFunc(kindsWithADefinitionOfTheirOwn, func(kind string) bool {
		return metricDefinitionsByKind[kind].declarationIn(model).IsUnknown()
	})
}

func isDeclared(value attr.Value) bool {
	return !value.IsNull() && !value.IsUnknown()
}

type metricDeclarationRefusal struct {
	attribute string
	summary   string
	detail    string
}

func (definition metricDefinitionOfItsOwnKind) refusalsOfOneEventAttributesBesideItIn(config metricModel) []metricDeclarationRefusal {
	attributesBesideIt := []struct {
		value   attr.Value
		refusal metricDeclarationRefusal
	}{
		{config.EventName, metricDeclarationRefusal{"event_name", invalidMetricDefinition, definition.refusalOfAnEventNameBesideIt}},
		{config.Filter, metricDeclarationRefusal{"filter", invalidMetricDefinition, definition.refusalOfAFilterBesideIt}},
		{config.ValueField, metricDeclarationRefusal{"value_field", invalidMetricDefinition, definition.refusalOfAValueFieldBesideIt}},
		{config.Kinds, metricDeclarationRefusal{"kinds", "Invalid kinds", definition.refusalOfKindsBesideIt}},
	}
	var refusals []metricDeclarationRefusal
	for _, attribute := range attributesBesideIt {
		if isDeclared(attribute.value) {
			refusals = append(refusals, attribute.refusal)
		}
	}
	return refusals
}

func refusalsOfMetricDeclaration(config metricModel) []metricDeclarationRefusal {
	declaredKinds := kindsOfTheDefinitionsDeclaredIn(config)
	switch {
	case len(declaredKinds) > 1:
		return []metricDeclarationRefusal{{
			attribute: declaredKinds[len(declaredKinds)-1],
			summary:   invalidMetricDefinition,
			detail:    "A metric is a ratio, a funnel or a retention metric, never two of them. Set one of the three.",
		}}
	case len(declaredKinds) == 1:
		return metricDefinitionsByKind[declaredKinds[0]].refusalsOfOneEventAttributesBesideItIn(config)
	case isADefinitionUnknownIn(config):
		return nil
	case config.EventName.IsNull():
		return []metricDeclarationRefusal{{
			attribute: "event_name",
			summary:   "Missing metric definition",
			detail:    "A metric needs an event_name, a ratio, a funnel or a retention.",
		}}
	case config.Kinds.IsNull():
		return []metricDeclarationRefusal{{
			attribute: "kinds",
			summary:   "Missing kinds",
			detail:    "A metric with an event_name needs kinds, at least one of: " + strings.Join(oneEventMetricKinds, ", ") + ".",
		}}
	default:
		return nil
	}
}

type metricDeclaredAsOneEventNameOrOneDefinition struct{}

func (v metricDeclaredAsOneEventNameOrOneDefinition) Description(ctx context.Context) string {
	return "a metric has an event_name, a ratio, a funnel or a retention, and only an event_name takes a filter, a value_field and kinds"
}

func (v metricDeclaredAsOneEventNameOrOneDefinition) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v metricDeclaredAsOneEventNameOrOneDefinition) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config metricModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, refusal := range refusalsOfMetricDeclaration(config) {
		resp.Diagnostics.AddAttributeError(path.Root(refusal.attribute), refusal.summary, refusal.detail)
	}
}

type kindsAreThoseOfAMetricOfOneEventName struct{}

func (v kindsAreThoseOfAMetricOfOneEventName) Description(ctx context.Context) string {
	return "the kinds of a metric with an event_name are among: " + strings.Join(oneEventMetricKinds, ", ")
}

func (v kindsAreThoseOfAMetricOfOneEventName) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v kindsAreThoseOfAMetricOfOneEventName) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	var config metricModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || len(kindsOfTheDefinitionsDeclaredIn(config)) > 0 {
		return
	}

	setvalidator.ValueStringsAre(stringvalidator.OneOf(oneEventMetricKinds...)).ValidateSet(ctx, req, resp)
}

func metricDefinitionEventNameAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Required:    true,
		Description: description,
		Validators:  []validator.String{stringvalidator.UTF8LengthBetween(1, eventNameMaximumCharacters)},
	}
}

type kindOfTheDeclaredDefinition struct{}

func (m kindOfTheDeclaredDefinition) Description(ctx context.Context) string {
	return "a ratio, funnel or retention metric reports its own kind alone"
}

func (m kindOfTheDeclaredDefinition) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m kindOfTheDeclaredDefinition) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.Plan.Raw.IsNull() || !req.ConfigValue.IsNull() {
		return
	}
	var plan metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	declaredKinds := kindsOfTheDefinitionsDeclaredIn(plan)
	if len(declaredKinds) == 1 {
		resp.PlanValue = types.SetValueMust(types.StringType, []attr.Value{types.StringValue(declaredKinds[0])})
	}
}
