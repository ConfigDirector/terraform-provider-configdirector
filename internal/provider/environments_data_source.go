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

var _ datasource.DataSource = &EnvironmentsDataSource{}

func NewEnvironmentsDataSource() datasource.DataSource {
	return &EnvironmentsDataSource{}
}

type EnvironmentsDataSource struct {
	client *client.Client
}

func (d *EnvironmentsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environments"
}

func (d *EnvironmentsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := EnvironmentsDataSourceSchema(ctx)
	s.Attributes["project_id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the project whose environments should be listed.",
	}
	resp.Schema = s
}

func (d *EnvironmentsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state EnvironmentsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	envs, err := d.client.ListEnvironments(ctx, state.ProjectId.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Environments", err.Error())
		return
	}

	elemType := EnvironmentsDsItemValue{}.Type(ctx)
	elems := make([]attr.Value, len(envs))
	for i, e := range envs {
		elems[i] = NewEnvironmentsDsItemValueMust(
			EnvironmentsDsItemValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"color":      stringValue(e.Color),
				"created_at": stringValue(e.CreatedAt),
				"id":         stringValue(e.ID),
				"live":       boolValue(e.Live),
				"name":       stringValue(e.Name),
				"project_id": stringValue(e.ProjectID),
				"slug":       stringValue(e.Slug),
				"updated_at": stringValue(e.UpdatedAt),
			},
		)
	}
	setVal, diags := types.SetValue(elemType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Environments = setVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
