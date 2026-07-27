package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const defaultBaseUrl = "https://api.configdirector.com"

var (
	_ provider.Provider              = &ConfigDirectorProvider{}
	_ provider.ProviderWithFunctions = &ConfigDirectorProvider{}
)

type ConfigDirectorProvider struct {
	version string
}

type configDirectorProviderModel struct {
	BaseUrl types.String `tfsdk:"base_url"`
	Token   types.String `tfsdk:"token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ConfigDirectorProvider{version: version}
	}
}

func (p *ConfigDirectorProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "configdirector"
	resp.Version = p.version
}

func (p *ConfigDirectorProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the ConfigDirector API, e.g. https://api.configdirector.com/v1. Defaults to the CONFIGDIRECTOR_BASE_URL environment variable, falling back to " + defaultBaseUrl + ".",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bearer token used to authenticate with the ConfigDirector API. Defaults to the CONFIGDIRECTOR_TOKEN environment variable.",
			},
		},
	}
}

func (p *ConfigDirectorProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data configDirectorProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	baseUrl := os.Getenv("CONFIGDIRECTOR_BASE_URL")
	if !data.BaseUrl.IsNull() && data.BaseUrl.ValueString() != "" {
		baseUrl = data.BaseUrl.ValueString()
	}
	if baseUrl == "" {
		baseUrl = defaultBaseUrl
	}

	token := os.Getenv("CONFIGDIRECTOR_TOKEN")
	if !data.Token.IsNull() && data.Token.ValueString() != "" {
		token = data.Token.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddError(
			"Missing API Token",
			"The ConfigDirector API token was not found. Set the token provider attribute or the CONFIGDIRECTOR_TOKEN environment variable.",
		)
		return
	}

	c := client.New(baseUrl, token)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *ConfigDirectorProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewEnvironmentResource,
		NewConfigResource,
		NewConfigTargetingRulesResource,
	}
}

func (p *ConfigDirectorProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProjectDataSource,
		NewProjectsDataSource,
		NewEnvironmentDataSource,
		NewEnvironmentsDataSource,
		NewConfigDataSource,
		NewConfigsDataSource,
	}
}

func (p *ConfigDirectorProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		NewRuleIDFunction,
	}
}
