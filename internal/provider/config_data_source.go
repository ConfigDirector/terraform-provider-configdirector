package provider

import (
	"context"
	"fmt"

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

type ConfigDataSourceModel struct {
	Id             types.String `tfsdk:"id"`
	ProjectId      types.String `tfsdk:"project_id"`
	Key            types.String `tfsdk:"key"`
	Description    types.String `tfsdk:"description"`
	Role           types.String `tfsdk:"role"`
	Lifetime       types.String `tfsdk:"lifetime"`
	Type           types.String `tfsdk:"type"`
	State          types.String `tfsdk:"state"`
	Client         types.Bool   `tfsdk:"client"`
	Server         types.Bool   `tfsdk:"server"`
	DeprecatedKeys types.List   `tfsdk:"deprecated_keys"`
}

func (d *ConfigDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config"
}

func (d *ConfigDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the config to look up.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the project this config belongs to.",
			},
			"id":          schema.StringAttribute{Computed: true},
			"description": schema.StringAttribute{Computed: true},
			"role":        schema.StringAttribute{Computed: true},
			"lifetime":    schema.StringAttribute{Computed: true},
			"type":        schema.StringAttribute{Computed: true},
			"state":       schema.StringAttribute{Computed: true},
			"client":      schema.BoolAttribute{Computed: true},
			"server":      schema.BoolAttribute{Computed: true},
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
	}
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

	keysList, diags := deprecatedKeysListValue(ctx, cfg.DeprecatedKeys)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.DeprecatedKeys = keysList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
