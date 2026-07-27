package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &ProjectDataSource{}

func NewProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

type ProjectDataSource struct {
	client *client.Client
}

type ProjectDataSourceModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Slug           types.String `tfsdk:"slug"`
	Environments   types.Set    `tfsdk:"environments"`
}

func (d *ProjectDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the project to look up.",
			},
			"name":            schema.StringAttribute{Computed: true},
			"organization_id": schema.StringAttribute{Computed: true},
			"slug":            schema.StringAttribute{Computed: true},
			// A Set, not a List: see project_resource.go's "environments"
			// attribute for why.
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

func (d *ProjectDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := d.client.GetProject(ctx, state.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Project", err.Error())
		return
	}

	state.Id = stringValue(project.ID)
	state.Name = stringValue(project.Name)
	state.Slug = stringValue(project.Slug)
	state.OrganizationId = stringValue(project.OrganizationID)

	envSet, diags := environmentSummarySetValue(ctx, project.Environments)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Environments = envSet

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
