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

var _ datasource.DataSource = &ProjectDataSource{}

func NewProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

type ProjectDataSource struct {
	client *client.Client
}

func (d *ProjectDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := ProjectDataSourceSchema(ctx)
	s.Attributes["id"] = schema.StringAttribute{
		Required:    true,
		Description: "ID of the project to look up.",
	}
	resp.Schema = s
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

	elemType := EnvironmentsValue{}.Type(ctx)
	elems := make([]attr.Value, len(project.Environments))
	for i, e := range project.Environments {
		elems[i] = NewEnvironmentsValueMust(
			EnvironmentsValue{}.AttributeTypes(ctx),
			map[string]attr.Value{
				"color":      stringValue(e.Color),
				"id":         stringValue(e.ID),
				"live":       boolValue(e.Live),
				"name":       stringValue(e.Name),
				"project_id": stringValue(e.ProjectID),
				"slug":       stringValue(e.Slug),
			},
		)
	}
	envList, diags := types.ListValue(elemType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Environments = envList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
