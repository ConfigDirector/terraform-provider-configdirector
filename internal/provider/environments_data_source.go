package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &EnvironmentsDataSource{}

func NewEnvironmentsDataSource() datasource.DataSource {
	return &EnvironmentsDataSource{}
}

type EnvironmentsDataSource struct {
	client *client.Client
}

type EnvironmentsModel struct {
	ProjectId    types.String `tfsdk:"project_id"`
	Environments types.Set    `tfsdk:"environments"`
}

func (d *EnvironmentsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environments"
}

func (d *EnvironmentsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the project whose environments should be listed.",
			},
			"environments": schema.SetNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"color":      schema.StringAttribute{Computed: true},
						"id":         schema.StringAttribute{Computed: true},
						"live":       schema.BoolAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"project_id": schema.StringAttribute{Computed: true},
						"slug":       schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
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

	setVal, diags := environmentSummarySetValue(ctx, envs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Environments = setVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
