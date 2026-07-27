package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &ConfigsDataSource{}

func NewConfigsDataSource() datasource.DataSource {
	return &ConfigsDataSource{}
}

type ConfigsDataSource struct {
	client *client.Client
}

type ConfigsModel struct {
	ProjectId types.String `tfsdk:"project_id"`
	Configs   types.Set    `tfsdk:"configs"`
}

func (d *ConfigsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configs"
}

func (d *ConfigsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the project whose configs should be listed.",
			},
			"configs": schema.SetNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true},
						"key":         schema.StringAttribute{Computed: true},
						"description": schema.StringAttribute{Computed: true},
						"role":        schema.StringAttribute{Computed: true},
						"lifetime":    schema.StringAttribute{Computed: true},
						"type":        schema.StringAttribute{Computed: true},
						"state":       schema.StringAttribute{Computed: true},
						"client":      schema.BoolAttribute{Computed: true},
						"server":      schema.BoolAttribute{Computed: true},
						"project_id":  schema.StringAttribute{Computed: true},
						"deprecated_keys": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"config_id":  schema.StringAttribute{Computed: true},
									"id":         schema.StringAttribute{Computed: true},
									"is_primary": schema.BoolAttribute{Computed: true},
									"key":        schema.StringAttribute{Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
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

	models := make([]configSummaryModel, len(configs))
	for i, cfg := range configs {
		keysList, diags := deprecatedKeysListValue(ctx, cfg.DeprecatedKeys)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		models[i] = configSummaryModel{
			Client:         boolValue(cfg.Client),
			DeprecatedKeys: keysList,
			Description:    stringPtrValue(cfg.Description),
			Id:             stringValue(cfg.ID),
			Key:            stringValue(cfg.Key),
			Lifetime:       stringValue(cfg.Lifetime),
			ProjectId:      stringValue(cfg.ProjectID),
			Role:           stringValue(cfg.Role),
			Server:         boolValue(cfg.Server),
			State:          stringValue(cfg.State),
			Type:           stringValue(cfg.Type),
		}
	}
	setVal, diags := types.SetValueFrom(ctx, types.ObjectType{AttrTypes: configSummaryAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Configs = setVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
