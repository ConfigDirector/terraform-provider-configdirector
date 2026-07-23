package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var _ datasource.DataSource = &ProjectsDataSource{}

func NewProjectsDataSource() datasource.DataSource {
	return &ProjectsDataSource{}
}

type ProjectsDataSource struct {
	client *client.Client
}

func (d *ProjectsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}

func (d *ProjectsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = ProjectsDataSourceSchema(ctx)
}

func (d *ProjectsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ProjectsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectsModel

	projects, err := d.client.ListProjects(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Projects", err.Error())
		return
	}

	elemType := ProjectsValue{}.Type(ctx)
	elems := make([]attr.Value, len(projects))
	for i, p := range projects {
		elems[i] = NewProjectsValueMust(
			ProjectsValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"created_at":      stringValue(p.CreatedAt),
				"id":              stringValue(p.ID),
				"name":            stringValue(p.Name),
				"organization_id": stringValue(p.OrganizationID),
				"slug":            stringValue(p.Slug),
				"updated_at":      stringValue(p.UpdatedAt),
			},
		)
	}
	setVal, diags := types.SetValue(elemType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Projects = setVal

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
