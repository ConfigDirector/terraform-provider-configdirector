package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &ConfigsDataSource{}

func NewConfigsDataSource() datasource.DataSource {
	return &ConfigsDataSource{}
}

type ConfigsDataSource struct {
	client *client.Client
}

func (d *ConfigsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configs"
}

func (d *ConfigsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := ConfigsDataSourceSchema(ctx)
	s.Attributes["project_id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the project whose configs should be listed.",
	}
	resp.Schema = s
}

func (d *ConfigsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	d.client = c
}

func configsDeprecatedKeysListValue(ctx context.Context, keys []client.DeprecatedKey) (types.List, error) {
	elemType := ConfigsDsDeprecatedKeysValue{}.Type(ctx)
	elems := make([]attr.Value, len(keys))
	for i, k := range keys {
		elems[i] = NewConfigsDsDeprecatedKeysValueMust(
			ConfigsDsDeprecatedKeysValue{}.AttributeTypes(ctx),
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

func (d *ConfigsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ConfigsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configs, err := d.client.ListConfigs(ctx, state.ProjectId.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Configs", err.Error())
		return
	}

	elemType := ConfigsValue{}.Type(ctx)
	elems := make([]attr.Value, len(configs))
	for i, cfg := range configs {
		keysList, err := configsDeprecatedKeysListValue(ctx, cfg.DeprecatedKeys)
		if err != nil {
			resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
			return
		}
		elems[i] = NewConfigsValueMust(
			ConfigsValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"client":          boolValue(cfg.Client),
				"created_at":      stringValue(cfg.CreatedAt),
				"deprecated_keys": keysList,
				"description":     stringPtrValue(cfg.Description),
				"id":              stringValue(cfg.ID),
				"key":             stringValue(cfg.Key),
				"lifetime":        stringValue(cfg.Lifetime),
				"project_id":      stringValue(cfg.ProjectID),
				"role":            stringValue(cfg.Role),
				"server":          boolValue(cfg.Server),
				"state":           stringValue(cfg.State),
				"type":            stringValue(cfg.Type),
				"updated_at":      stringValue(cfg.UpdatedAt),
			},
		)
	}
	setVal, diags := types.SetValue(elemType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Configs = setVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
