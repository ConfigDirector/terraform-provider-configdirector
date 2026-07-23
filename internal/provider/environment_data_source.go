package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &EnvironmentDataSource{}

func NewEnvironmentDataSource() datasource.DataSource {
	return &EnvironmentDataSource{}
}

type EnvironmentDataSource struct {
	client *client.Client
}

func (d *EnvironmentDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *EnvironmentDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := EnvironmentDataSourceSchema(ctx)
	s.Attributes["id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the environment to look up.",
	}
	s.Attributes["project_id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the project this environment belongs to.",
	}
	resp.Schema = s
}

func (d *EnvironmentDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state EnvironmentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := d.client.GetEnvironment(ctx, state.ProjectId.ValueString(), state.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Environment", err.Error())
		return
	}

	state.Id = stringValue(env.ID)
	state.Name = stringValue(env.Name)
	state.Slug = stringValue(env.Slug)
	state.Color = stringValue(env.Color)
	state.ProjectId = stringValue(env.ProjectID)
	state.CreatedAt = stringValue(env.CreatedAt)
	state.UpdatedAt = stringValue(env.UpdatedAt)
	state.Live = boolValue(env.Live)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
