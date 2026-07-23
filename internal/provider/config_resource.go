package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &ConfigResource{}
	_ resource.ResourceWithImportState = &ConfigResource{}
)

func NewConfigResource() resource.Resource {
	return &ConfigResource{}
}

type ConfigResource struct {
	client *client.Client
}

// ConfigResourceModel mirrors the generated ConfigModel with one addition:
// DefaultValue. The codegen tool can't represent config's defaultValue field
// (a boolean|string|number union), so it's added by hand as a JSON-encoded
// string; every schema attribute needs a matching tfsdk-tagged struct field
// or Plan.Get/State.Set fail at runtime, so this can't just reuse ConfigModel.
type ConfigResourceModel struct {
	Client         types.Bool   `tfsdk:"client"`
	CreatedAt      types.String `tfsdk:"created_at"`
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
	UpdatedAt      types.String `tfsdk:"updated_at"`
	DefaultValue   types.String `tfsdk:"default_value"`
}

func (r *ConfigResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config"
}

// The OpenAPI-driven codegen cannot model config's defaultValue/typeOptions/
// variations/targets fields: they're polymorphic (anyOf) unions the codegen
// tool doesn't support (see generator_config.yml ignores). default_value is
// added back by hand as a JSON-encoded string, since the API requires it on
// create; it has no update path, so changes require replacement.
func (r *ConfigResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	s := ConfigResourceSchema(ctx)

	s.Attributes["project_id"] = schema.StringAttribute{
		Required:      true,
		Description:   "ID of the project this config belongs to.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
	s.Attributes["id"] = schema.StringAttribute{
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
	s.Attributes["default_value"] = schema.StringAttribute{
		Required: true,
		Description: "JSON-encoded default value for this config (e.g. \"true\", \"42\", \"\\\"some string\\\"\"). " +
			"The ConfigDirector API has no endpoint that returns the global default back, so this value is " +
			"write-only from Terraform's perspective: it is sent on create and never reconciled against server " +
			"state, and cannot be changed in place (any change forces replacement).",
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}

	resp.Schema = s
}

func (r *ConfigResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func deprecatedKeysListValue(ctx context.Context, keys []client.DeprecatedKey) (types.List, error) {
	elemType := DeprecatedKeysValue{}.Type(ctx)
	elems := make([]attr.Value, len(keys))
	for i, k := range keys {
		elems[i] = NewDeprecatedKeysValueMust(
			DeprecatedKeysValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"config_id":  stringValue(k.ConfigID),
				"id":         stringValue(k.ID),
				"is_primary": boolValue(k.IsPrimary),
				"key":        stringValue(k.Key),
			},
		)
	}
	listVal, diags := types.ListValue(elemType, elems)
	if diags.HasError() {
		return types.ListNull(elemType), fmt.Errorf("%v", diags)
	}
	return listVal, nil
}

// configToModel populates every field except DefaultValue, which the API
// never returns; callers must set/preserve it separately.
func configToModel(ctx context.Context, c *client.Config, m *ConfigResourceModel) error {
	m.Id = stringValue(c.ID)
	m.ProjectId = stringValue(c.ProjectID)
	m.Key = stringValue(c.Key)
	m.Description = stringPtrValue(c.Description)
	m.Role = stringValue(c.Role)
	m.Lifetime = stringValue(c.Lifetime)
	m.Type = stringValue(c.Type)
	m.State = stringValue(c.State)
	m.Client = boolValue(c.Client)
	m.Server = boolValue(c.Server)
	m.CreatedAt = stringValue(c.CreatedAt)
	m.UpdatedAt = stringValue(c.UpdatedAt)

	keysList, err := deprecatedKeysListValue(ctx, c.DeprecatedKeys)
	if err != nil {
		return err
	}
	m.DeprecatedKeys = keysList
	return nil
}

func (r *ConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var defaultVal any
	if err := jsonUnmarshalString(plan.DefaultValue.ValueString(), &defaultVal); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("default_value"), "Invalid default_value", err.Error())
		return
	}

	server := plan.Server.ValueBool()
	clientFlag := plan.Client.ValueBool()
	cfg, err := r.client.CreateConfig(ctx, plan.ProjectId.ValueString(), client.CreateConfigRequest{
		Key:          plan.Key.ValueString(),
		Description:  stringPtrFromValue(plan.Description),
		Role:         plan.Role.ValueString(),
		Lifetime:     plan.Lifetime.ValueString(),
		Type:         plan.Type.ValueString(),
		Server:       &server,
		Client:       &clientFlag,
		DefaultValue: defaultVal,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &plan); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.GetConfig(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &state); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.UpdateConfig(ctx, plan.ProjectId.ValueString(), state.Key.ValueString(), client.UpdateConfigRequest{
		Key:         plan.Key.ValueString(),
		Description: stringPtrFromValue(plan.Description),
		Role:        plan.Role.ValueString(),
		Lifetime:    plan.Lifetime.ValueString(),
		Type:        plan.Type.ValueString(),
		Server:      plan.Server.ValueBool(),
		Client:      plan.Client.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &plan); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteConfig(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Config", err.Error())
	}
}

func (r *ConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id/key. Got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[1])...)
	resp.Diagnostics.AddWarning(
		"default_value Not Imported",
		"The ConfigDirector API does not return a config's default value, so default_value cannot be populated by import. Set it manually after import or the next plan will show a replacement.",
	)
}
