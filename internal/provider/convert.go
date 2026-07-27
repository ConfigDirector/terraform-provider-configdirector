package provider

import (
	"context"
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

func stringValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func stringPtrValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func boolValue(b bool) types.Bool {
	return types.BoolValue(b)
}

func stringPtrFromValue(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// Nested object models below back every ListNestedAttribute/SetNestedAttribute
// in the schema. None of them declare a schema.NestedAttributeObject.CustomType:
// left unset, the framework derives the object type from the Attributes map,
// which lines up with these structs' tfsdk tags, so types.ListValueFrom /
// types.SetValueFrom can build the attribute value directly from a slice of
// plain structs with no hand-rolled ObjectType/ObjectValuable implementation.

// environmentSummaryModel is the shape of an environment as it appears
// embedded in a project's "environments" attribute and in the standalone
// "environments" list data source - identical in both places.
type environmentSummaryModel struct {
	Color     types.String `tfsdk:"color"`
	Id        types.String `tfsdk:"id"`
	Live      types.Bool   `tfsdk:"live"`
	Name      types.String `tfsdk:"name"`
	ProjectId types.String `tfsdk:"project_id"`
	Slug      types.String `tfsdk:"slug"`
}

var environmentSummaryAttrTypes = map[string]attr.Type{
	"color":      types.StringType,
	"id":         types.StringType,
	"live":       types.BoolType,
	"name":       types.StringType,
	"project_id": types.StringType,
	"slug":       types.StringType,
}

func environmentSummaryModelFromAPI(e client.Environment) environmentSummaryModel {
	return environmentSummaryModel{
		Color:     stringValue(e.Color),
		Id:        stringValue(e.ID),
		Live:      boolValue(e.Live),
		Name:      stringValue(e.Name),
		ProjectId: stringValue(e.ProjectID),
		Slug:      stringValue(e.Slug),
	}
}

func environmentSummarySetValue(ctx context.Context, envs []client.Environment) (types.Set, diag.Diagnostics) {
	models := make([]environmentSummaryModel, len(envs))
	for i, e := range envs {
		models[i] = environmentSummaryModelFromAPI(e)
	}
	return types.SetValueFrom(ctx, types.ObjectType{AttrTypes: environmentSummaryAttrTypes}, models)
}

// deprecatedKeyModel is the shape of a config's "deprecated_keys" entries,
// wherever a config or list of configs is read.
type deprecatedKeyModel struct {
	ConfigId  types.String `tfsdk:"config_id"`
	Id        types.String `tfsdk:"id"`
	IsPrimary types.Bool   `tfsdk:"is_primary"`
	Key       types.String `tfsdk:"key"`
}

var deprecatedKeyAttrTypes = map[string]attr.Type{
	"config_id":  types.StringType,
	"id":         types.StringType,
	"is_primary": types.BoolType,
	"key":        types.StringType,
}

func deprecatedKeysListValue(ctx context.Context, keys []client.DeprecatedKey) (types.List, diag.Diagnostics) {
	models := make([]deprecatedKeyModel, len(keys))
	for i, k := range keys {
		models[i] = deprecatedKeyModel{
			ConfigId:  stringValue(k.ConfigID),
			Id:        stringValue(k.ID),
			IsPrimary: boolValue(k.IsPrimary),
			Key:       stringValue(k.Key),
		}
	}
	return types.ListValueFrom(ctx, types.ObjectType{AttrTypes: deprecatedKeyAttrTypes}, models)
}

// projectSummaryModel is the shape of a project as it appears in the
// "projects" list data source.
type projectSummaryModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Slug           types.String `tfsdk:"slug"`
}

var projectSummaryAttrTypes = map[string]attr.Type{
	"id":              types.StringType,
	"name":            types.StringType,
	"organization_id": types.StringType,
	"slug":            types.StringType,
}

// configSummaryModel is the shape of a config as it appears in the "configs"
// list data source.
type configSummaryModel struct {
	Client         types.Bool   `tfsdk:"client"`
	DeprecatedKeys types.List   `tfsdk:"deprecated_keys"`
	Description    types.String `tfsdk:"description"`
	Id             types.String `tfsdk:"id"`
	Key            types.String `tfsdk:"key"`
	Lifetime       types.String `tfsdk:"lifetime"`
	ProjectId      types.String `tfsdk:"project_id"`
	Role           types.String `tfsdk:"role"`
	Server         types.Bool   `tfsdk:"server"`
	State          types.String `tfsdk:"state"`
	Type           types.String `tfsdk:"type"`
}

var configSummaryAttrTypes = map[string]attr.Type{
	"client":          types.BoolType,
	"deprecated_keys": types.ListType{ElemType: types.ObjectType{AttrTypes: deprecatedKeyAttrTypes}},
	"description":     types.StringType,
	"id":              types.StringType,
	"key":             types.StringType,
	"lifetime":        types.StringType,
	"project_id":      types.StringType,
	"role":            types.StringType,
	"server":          types.BoolType,
	"state":           types.StringType,
	"type":            types.StringType,
}

// dynamicFromJSON converts an arbitrary JSON value (as produced by
// encoding/json's decode-into-`any`: nil, bool, float64, string, []any,
// map[string]any) into a types.Dynamic. Only the outermost value is wrapped
// as dynamic; every nested slot gets a concrete type (Bool/Number/String/
// Tuple/Object) inferred from the value present, matching how Terraform
// itself builds a dynamic attribute's value out of a literal HCL object or
// tuple. A field like config's typeOptions has a genuinely different shape
// per config type (integer bounds vs. enum values vs. timespan units, ...),
// which is why the attribute is dynamic at all - but wrapping every nested
// level in its own dynamic type (rather than just the top) produces a value
// shape Terraform's own planned value never matches, which surfaces as
// "provider produced inconsistent result" on every apply.
func dynamicFromJSON(v any) (types.Dynamic, error) {
	if v == nil {
		return types.DynamicNull(), nil
	}
	val, err := attrValueFromJSON(v)
	if err != nil {
		return types.DynamicNull(), err
	}
	return types.DynamicValue(val), nil
}

// attrValueFromJSON returns a concretely-typed attr.Value for any JSON value
// except nil, which is represented as a dynamic null since there's no way to
// infer a concrete type for an absent value.
func attrValueFromJSON(v any) (attr.Value, error) {
	switch x := v.(type) {
	case nil:
		return types.DynamicNull(), nil
	case bool:
		return types.BoolValue(x), nil
	case float64:
		return types.NumberValue(big.NewFloat(x)), nil
	case string:
		return types.StringValue(x), nil
	case []any:
		elemValues := make([]attr.Value, len(x))
		elemTypes := make([]attr.Type, len(x))
		for i, item := range x {
			val, err := attrValueFromJSON(item)
			if err != nil {
				return nil, err
			}
			elemValues[i] = val
			elemTypes[i] = val.Type(context.Background())
		}
		tupleVal, diags := types.TupleValue(elemTypes, elemValues)
		if diags.HasError() {
			return nil, fmt.Errorf("%v", diags)
		}
		return tupleVal, nil
	case map[string]any:
		attrTypes := make(map[string]attr.Type, len(x))
		attrValues := make(map[string]attr.Value, len(x))
		for k, item := range x {
			val, err := attrValueFromJSON(item)
			if err != nil {
				return nil, err
			}
			attrValues[k] = val
			attrTypes[k] = val.Type(context.Background())
		}
		objVal, diags := types.ObjectValue(attrTypes, attrValues)
		if diags.HasError() {
			return nil, fmt.Errorf("%v", diags)
		}
		return objVal, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value type %T", v)
	}
}

// jsonFromDynamic is the inverse of dynamicFromJSON: it converts a
// types.Dynamic (as built above, or as constructed from HCL by Terraform)
// back into a plain Go value ready for encoding/json.
func jsonFromDynamic(d types.Dynamic) (any, error) {
	if d.IsNull() || d.IsUnknown() {
		return nil, nil
	}
	return jsonFromAttrValue(d.UnderlyingValue())
}

func jsonFromAttrValue(v attr.Value) (any, error) {
	switch x := v.(type) {
	case types.Dynamic:
		if x.IsNull() || x.IsUnknown() {
			return nil, nil
		}
		return jsonFromAttrValue(x.UnderlyingValue())
	case types.Bool:
		if x.IsNull() || x.IsUnknown() {
			return nil, nil
		}
		return x.ValueBool(), nil
	case types.String:
		if x.IsNull() || x.IsUnknown() {
			return nil, nil
		}
		return x.ValueString(), nil
	case types.Number:
		if x.IsNull() || x.IsUnknown() {
			return nil, nil
		}
		f, _ := x.ValueBigFloat().Float64()
		return f, nil
	case types.Tuple:
		elems := x.Elements()
		out := make([]any, len(elems))
		for i, e := range elems {
			val, err := jsonFromAttrValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	case types.Object:
		attrsMap := x.Attributes()
		out := make(map[string]any, len(attrsMap))
		for k, e := range attrsMap {
			val, err := jsonFromAttrValue(e)
			if err != nil {
				return nil, err
			}
			out[k] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported attr.Value type %T", v)
	}
}

// Variations is modeled as a single top-level types.Dynamic covering the
// whole array, not a ListNestedAttribute with a per-item dynamic "value":
// terraform-plugin-framework doesn't support a dynamic type nested inside a
// collection type ("Dynamic types inside of collections are not currently
// supported"). dynamicFromJSON/jsonFromDynamic already handle arbitrary
// nested JSON generically, so variationsToDynamicJSON/variationsFromDynamic
// just reshape between []client.Variation and the plain
// []any{map[string]any{...}} shape those functions expect.

// variationsToDynamicJSON only sets the "name" key when present. Whether the
// user's HCL included a "name" for a given variation determines that
// element's object type (an object with vs. without a "name" attribute);
// always including the key here (even as null) would produce a shape that
// doesn't match what Terraform parsed from a config that omitted it,
// surfacing as "provider produced inconsistent result".
func variationsToDynamicJSON(variations []client.Variation) any {
	if variations == nil {
		return nil
	}
	out := make([]any, len(variations))
	for i, v := range variations {
		m := map[string]any{"value": v.Value}
		if v.Name != nil {
			m["name"] = *v.Name
		}
		out[i] = m
	}
	return out
}

func variationsFromDynamic(d types.Dynamic) ([]client.Variation, error) {
	raw, err := jsonFromDynamic(d)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("variations must be a list of objects, got %T", raw)
	}
	out := make([]client.Variation, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("each variation must be an object, got %T", item)
		}
		v := client.Variation{Value: m["value"]}
		if name, ok := m["name"].(string); ok {
			v.Name = &name
		}
		out[i] = v
	}
	return out, nil
}
