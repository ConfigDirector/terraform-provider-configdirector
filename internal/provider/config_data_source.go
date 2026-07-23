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

var _ datasource.DataSource = &ConfigDataSource{}

func NewConfigDataSource() datasource.DataSource {
	return &ConfigDataSource{}
}

type ConfigDataSource struct {
	client *client.Client
}

func (d *ConfigDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config"
}

func (d *ConfigDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := ConfigDataSourceSchema(ctx)
	s.Attributes["key"] = schema.StringAttribute{
		Required:    true,
		Description: "Key of the config to look up.",
	}
	s.Attributes["project_id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the project this config belongs to.",
	}
	resp.Schema = s
}

func (d *ConfigDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ConfigDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ConfigDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := d.client.GetConfig(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}

	state.Id = stringValue(cfg.ID)
	state.ProjectId = stringValue(cfg.ProjectID)
	state.Key = stringValue(cfg.Key)
	state.Description = stringPtrValue(cfg.Description)
	state.Role = stringValue(cfg.Role)
	state.Lifetime = stringValue(cfg.Lifetime)
	state.Type = stringValue(cfg.Type)
	state.State = stringValue(cfg.State)
	state.Client = boolValue(cfg.Client)
	state.Server = boolValue(cfg.Server)

	elemType := ConfigDsDeprecatedKeysValue{}.Type(ctx)
	elems := make([]attr.Value, len(cfg.DeprecatedKeys))
	for i, k := range cfg.DeprecatedKeys {
		elems[i] = NewConfigDsDeprecatedKeysValueMust(
			ConfigDsDeprecatedKeysValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"config_id":  stringValue(k.ConfigID),
				"id":         stringValue(k.ID),
				"is_primary": boolValue(k.IsPrimary),
				"key":        stringValue(k.Key),
			},
		)
	}
	listVal, diags := types.ListValue(elemType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.DeprecatedKeys = listVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
