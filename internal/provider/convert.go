package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

func jsonUnmarshalString(s string, out any) error {
	return json.Unmarshal([]byte(s), out)
}

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

func environmentSummaryListValue(ctx context.Context, envs []client.Environment) (types.List, diag.Diagnostics) {
	models := make([]environmentSummaryModel, len(envs))
	for i, e := range envs {
		models[i] = environmentSummaryModelFromAPI(e)
	}
	return types.ListValueFrom(ctx, types.ObjectType{AttrTypes: environmentSummaryAttrTypes}, models)
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
