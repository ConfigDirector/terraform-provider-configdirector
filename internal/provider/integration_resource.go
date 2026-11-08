package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                   = &integrationResource{}
	_ resource.ResourceWithImportState    = &integrationResource{}
	_ resource.ResourceWithValidateConfig = &integrationResource{}
)

type integrationSetting struct {
	apiField                    string
	sentenceWhenMissing         string
	description                 string
	secret                      bool
	savedWithoutTrailingSlashes bool
	usableByEveryKind           bool
}

var integrationSettingDefinitions = map[string]integrationSetting{
	"data_center": {
		apiField:            "dataCenter",
		sentenceWhenMissing: "The Amplitude data center must be us or eu",
		description:         "Amplitude: the data center your project is in, us or eu.",
	},
	"data_residency": {
		apiField:            "dataResidency",
		sentenceWhenMissing: "The Mixpanel data residency must be us, eu or in",
		description:         "Mixpanel: where your project's data is stored, us, eu or in.",
	},
	"site": {
		apiField:            "site",
		sentenceWhenMissing: "The Datadog site must be one of: datadoghq.com, us3.datadoghq.com, us5.datadoghq.com, datadoghq.eu, ap1.datadoghq.com",
		description:         "Datadog: your site, one of datadoghq.com, us3.datadoghq.com, us5.datadoghq.com, datadoghq.eu or ap1.datadoghq.com.",
	},
	"url": {
		apiField:                    "url",
		sentenceWhenMissing:         "The Grafana URL is required",
		description:                 "Grafana: the base URL of your instance. It must start with https:// and its host must resolve to a public address.",
		savedWithoutTrailingSlashes: true,
	},
	"host": {
		apiField:                    "host",
		sentenceWhenMissing:         "The PostHog host must be us, eu or the https:// URL of a self-hosted instance",
		description:                 "PostHog: us, eu or the https:// URL of a self-hosted instance, whose host must resolve to a public address.",
		savedWithoutTrailingSlashes: true,
	},
	"project_id": {
		apiField:            "projectId",
		sentenceWhenMissing: "The project id is required",
		description:         "Mixpanel and PostHog: the numeric ID of the project in that tool, not a ConfigDirector project.",
	},
	"api_key": {
		apiField:            "apiKey",
		sentenceWhenMissing: "The API key is required",
		description:         "Amplitude and Datadog: the API key.",
		secret:              true,
	},
	"secret_key": {
		apiField:            "secretKey",
		sentenceWhenMissing: "The secret key is required",
		description:         "Amplitude: the secret key.",
		secret:              true,
	},
	"service_account_username": {
		apiField:            "serviceAccountUsername",
		sentenceWhenMissing: "The service account username is required",
		description:         "Mixpanel: the service account's username.",
		secret:              true,
	},
	"service_account_secret": {
		apiField:            "serviceAccountSecret",
		sentenceWhenMissing: "The service account secret is required",
		description:         "Mixpanel: the service account's secret.",
		secret:              true,
	},
	"service_account_token": {
		apiField:            "serviceAccountToken",
		sentenceWhenMissing: "The service account token is required",
		description:         "Grafana: the service account token.",
		secret:              true,
	},
	"personal_api_key": {
		apiField:            "personalApiKey",
		sentenceWhenMissing: "The personal API key is required",
		description:         "PostHog: the personal API key.",
		secret:              true,
	},
	"endpoint_url": {
		apiField:          "endpointUrl",
		description:       "Any kind: a URL to post to instead of the one ConfigDirector composes from the other settings. It must start with https:// and its host must resolve to a public address.",
		usableByEveryKind: true,
	},
}

var integrationRequiredSettingsByKind = map[string][]string{
	"amplitude": {"data_center", "api_key", "secret_key"},
	"mixpanel":  {"data_residency", "project_id", "service_account_username", "service_account_secret"},
	"datadog":   {"site", "api_key"},
	"grafana":   {"url", "service_account_token"},
	"posthog":   {"host", "project_id", "personal_api_key"},
}

func integrationKinds() []string {
	return slices.Sorted(maps.Keys(integrationRequiredSettingsByKind))
}

func integrationSettingAttributes() map[string]schema.Attribute {
	attributes := make(map[string]schema.Attribute, len(integrationSettingDefinitions))
	for name, setting := range integrationSettingDefinitions {
		attributes[name] = schema.StringAttribute{
			Optional:    true,
			Sensitive:   setting.secret,
			Description: setting.description,
		}
	}
	return attributes
}

func integrationSettingTypes() map[string]attr.Type {
	settingTypes := make(map[string]attr.Type, len(integrationSettingDefinitions))
	for name := range integrationSettingDefinitions {
		settingTypes[name] = types.StringType
	}
	return settingTypes
}

func integrationSettingValue(settings types.Object, name string) types.String {
	value, isString := settings.Attributes()[name].(types.String)
	if !isString {
		return types.StringNull()
	}
	return value
}

func integrationSettingIsBlank(value types.String) bool {
	return value.IsNull() || (!value.IsUnknown() && trimJavaScriptWhitespace(value.ValueString()) == "")
}

func newIntegrationResource() resource.Resource {
	return &integrationResource{}
}

type integrationResource struct {
	client *client.Client
}

type integrationModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Kind           types.String `tfsdk:"kind"`
	Settings       types.Object `tfsdk:"settings"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	ProjectIds     types.Set    `tfsdk:"project_ids"`
	EnvironmentIds types.Set    `tfsdk:"environment_ids"`
	TagIds         types.Set    `tfsdk:"tag_ids"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (r *integrationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *integrationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An integration connects the organization to an analytics tool and marks its charts when an experiment's run starts, its split changes, a variation is shipped or the run is stopped. An organization can have up to 10, several of one kind if it needs them. " +
			"See https://docs.configdirector.com/integrations/analytics for what each tool needs.\n\n" +
			"Requires an API token with the integrations:manage permission. Import with the integration's ID.\n\n" +
			"ConfigDirector never returns a key, token or secret once it is saved, so Terraform notices when you change one in the configuration but not when one is changed outside Terraform. " +
			"If ConfigDirector disables the integration after three days of failed deliveries, the next apply enables it again while enabled is true.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The integration's ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The integration's name, up to 200 characters.",
			},
			"kind": schema.StringAttribute{
				Required:      true,
				Description:   "The tool the integration connects to: " + strings.Join(integrationKinds(), ", ") + ". Changing the kind replaces the integration, which deletes its delivery log.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf(integrationKinds()...),
				},
			},
			"settings": schema.SingleNestedAttribute{
				Required:    true,
				Description: "What the kind needs to reach the tool. Each setting below names the kinds that use it. Every kind's settings are required, except endpoint_url, and a setting another kind uses is refused.",
				Attributes:  integrationSettingAttributes(),
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the integration posts to the tool. Defaults to true. Disabling it abandons the deliveries still waiting for an attempt.",
				Default:     booldefault.StaticBool(true),
			},
			"project_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs of experiments in these projects. Leave it empty, as it is by default, to mark runs from every project.",
				Default:     stringSetDefault(nil),
			},
			"environment_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs in these environments. Leave it empty, as it is by default, to mark runs from every environment.",
				Default:     stringSetDefault(nil),
			},
			"tag_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs of experiments that have one of these tags. Leave it empty, as it is by default, to mark runs of every experiment.",
				Default:     stringSetDefault(nil),
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "When the integration was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the integration was last changed.",
			},
		},
	}
}

func (r *integrationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, isClient := req.ProviderData.(*client.Client)
	if !isClient {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = api
}

func (r *integrationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var kind types.String
	var settings types.Object
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("kind"), &kind)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("settings"), &settings)...)
	if resp.Diagnostics.HasError() || settings.IsNull() || settings.IsUnknown() {
		return
	}

	requiredSettings, kindExists := integrationRequiredSettingsByKind[kind.ValueString()]
	if !kindExists {
		return
	}

	for _, name := range slices.Sorted(maps.Keys(integrationSettingDefinitions)) {
		setting := integrationSettingDefinitions[name]
		value := integrationSettingValue(settings, name)
		isRequired := slices.Contains(requiredSettings, name)
		settingPath := path.Root("settings").AtName(name)
		switch {
		case isRequired && integrationSettingIsBlank(value):
			resp.Diagnostics.AddAttributeError(settingPath, "Missing integration setting", setting.sentenceWhenMissing)
		case !isRequired && !setting.usableByEveryKind && !value.IsNull():
			resp.Diagnostics.AddAttributeError(settingPath, "Setting of another integration kind", fmt.Sprintf("%s does not apply to the %s kind. Remove it from settings.", name, kind.ValueString()))
		default:
			continue
		}
	}
}

func integrationRequestFrom(ctx context.Context, model integrationModel, sendsCredentials bool) (client.IntegrationRequest, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	settings := map[string]string{}
	for name, setting := range integrationSettingDefinitions {
		value := integrationSettingValue(model.Settings, name)
		if !value.IsNull() && (!setting.secret || sendsCredentials) {
			settings[setting.apiField] = value.ValueString()
		}
	}

	request := client.IntegrationRequest{
		Name:           model.Name.ValueString(),
		Kind:           model.Kind.ValueString(),
		Settings:       settings,
		Enabled:        model.Enabled.ValueBool(),
		ProjectIDs:     stringsOfSet(ctx, model.ProjectIds, &diagnostics),
		EnvironmentIDs: stringsOfSet(ctx, model.EnvironmentIds, &diagnostics),
		TagIDs:         stringsOfSet(ctx, model.TagIds, &diagnostics),
	}
	return request, diagnostics
}

func integrationCredentialsChanged(planned, saved types.Object) bool {
	for name, setting := range integrationSettingDefinitions {
		if setting.secret && !integrationSettingValue(planned, name).Equal(integrationSettingValue(saved, name)) {
			return true
		}
	}
	return false
}

func savedInstanceURLMatches(declared types.String, saved string) bool {
	return !declared.IsNull() && !declared.IsUnknown() && strings.TrimRight(trimJavaScriptWhitespace(declared.ValueString()), "/") == saved
}

func integrationSettingStateValue(setting integrationSetting, declared types.String, saved *string) types.String {
	switch {
	case setting.secret:
		return declared
	case saved == nil:
		return types.StringNull()
	case setting.savedWithoutTrailingSlashes && savedInstanceURLMatches(declared, *saved):
		return declared
	default:
		return declaredOrSaved(declared, *saved)
	}
}

func integrationSettingsForState(integration *client.Integration, declared types.Object) (types.Object, diag.Diagnostics) {
	values := make(map[string]attr.Value, len(integrationSettingDefinitions))
	for name, setting := range integrationSettingDefinitions {
		values[name] = integrationSettingStateValue(setting, integrationSettingValue(declared, name), integration.Settings[setting.apiField])
	}
	return types.ObjectValue(integrationSettingTypes(), values)
}

func integrationToModel(ctx context.Context, integration *client.Integration, model *integrationModel) diag.Diagnostics {
	settings, diagnostics := integrationSettingsForState(integration, model.Settings)

	model.Id = stringValue(integration.ID)
	model.Name = declaredOrSaved(model.Name, integration.Name)
	model.Kind = stringValue(integration.Kind)
	model.Settings = settings
	model.Enabled = boolValue(integration.Enabled)
	model.ProjectIds = setOfStrings(ctx, integration.ProjectIDs, &diagnostics)
	model.EnvironmentIds = setOfStrings(ctx, integration.EnvironmentIDs, &diagnostics)
	model.TagIds = setOfStrings(ctx, integration.TagIDs, &diagnostics)
	model.CreatedAt = stringValue(integration.CreatedAt)
	model.UpdatedAt = stringValue(integration.UpdatedAt)
	return diagnostics
}

func (r *integrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan integrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sendsCredentials := true
	request, diagnostics := integrationRequestFrom(ctx, plan, sendsCredentials)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	integration, err := r.client.CreateIntegration(ctx, request)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Integration", err.Error())
		return
	}

	resp.Diagnostics.Append(integrationToModel(ctx, integration, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *integrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state integrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integration, err := r.client.GetIntegration(ctx, state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Integration", err.Error())
		return
	}

	resp.Diagnostics.Append(integrationToModel(ctx, integration, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *integrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state integrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sendsCredentials := integrationCredentialsChanged(plan.Settings, state.Settings)
	request, diagnostics := integrationRequestFrom(ctx, plan, sendsCredentials)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	integration, err := r.client.UpdateIntegration(ctx, state.Id.ValueString(), request)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Integration", err.Error())
		return
	}

	resp.Diagnostics.Append(integrationToModel(ctx, integration, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *integrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state integrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteIntegration(ctx, state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Integration", err.Error())
	}
}

func (r *integrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
