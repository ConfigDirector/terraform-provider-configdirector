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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                   = &integrationResource{}
	_ resource.ResourceWithImportState    = &integrationResource{}
	_ resource.ResourceWithValidateConfig = &integrationResource{}
	_ resource.ResourceWithModifyPlan     = &integrationResource{}
)

type integrationSetting struct {
	apiField                    string
	sentenceWhenMissing         string
	description                 string
	secret                      bool
	savedWithoutTrailingSlashes bool
	isSwitch                    bool
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
		apiField:    "endpointUrl",
		description: "Amplitude, Mixpanel, Datadog, Grafana and PostHog: a URL to post to instead of the one ConfigDirector composes from the other settings. It must start with https:// and its host must resolve to a public address.",
	},
	"webhook_url": {
		apiField:            "webhookUrl",
		sentenceWhenMissing: "The Slack webhook URL must have the form https://hooks.slack.com/services/…",
		description:         "Slack: the incoming webhook URL Slack created for the channel, in the form https://hooks.slack.com/services/…",
		secret:              true,
	},
	"message_prefix": {
		apiField:    "messagePrefix",
		description: "Slack: text that opens each message, up to 200 characters, such as @here or <!subteam^ID> with the ID of a user group, to notify people. Leave it out for messages without one. A resolved alert is posted without it.",
	},
	"post_resolved_alerts": {
		apiField:    "postResolvedAlerts",
		description: "Slack: whether an alert that resolves gets one line in the channel it was posted to. Defaults to true.",
		isSwitch:    true,
	},
}

type integrationKind struct {
	requiredSettings []string
	optionalSettings []string
	startingValues   map[string]attr.Value
}

func (kind integrationKind) takes(attribute string) bool {
	_, startsWithIt := kind.startingValues[attribute]
	return startsWithIt || slices.Contains(kind.requiredSettings, attribute) || slices.Contains(kind.optionalSettings, attribute)
}

var analyticsOptionalSettings = []string{"endpoint_url"}

var slackStartingEventTypes = []string{"config.created", "config.updated", "config.archived", "config.restored", "targeting_rules.updated"}

var slackStartingAlertKinds = []string{
	"config-not-found",
	"client-requested-server-only-config",
	"server-requested-client-only-config",
	"config-type-mismatch",
	"default-value-mismatch",
	"default-value-served-too-often",
}

const slackStartingMinimumPriority = "high"

var integrationKindDefinitions = map[string]integrationKind{
	"amplitude": {requiredSettings: []string{"data_center", "api_key", "secret_key"}, optionalSettings: analyticsOptionalSettings},
	"mixpanel":  {requiredSettings: []string{"data_residency", "project_id", "service_account_username", "service_account_secret"}, optionalSettings: analyticsOptionalSettings},
	"datadog":   {requiredSettings: []string{"site", "api_key"}, optionalSettings: analyticsOptionalSettings},
	"grafana":   {requiredSettings: []string{"url", "service_account_token"}, optionalSettings: analyticsOptionalSettings},
	"posthog":   {requiredSettings: []string{"host", "project_id", "personal_api_key"}, optionalSettings: analyticsOptionalSettings},
	"slack": {
		requiredSettings: []string{"webhook_url"},
		optionalSettings: []string{"message_prefix"},
		startingValues: map[string]attr.Value{
			"post_resolved_alerts": types.BoolValue(true),
			"event_types":          stringSetValue(slackStartingEventTypes),
			"alert_kinds":          stringSetValue(slackStartingAlertKinds),
			"minimum_priority":     types.StringValue(slackStartingMinimumPriority),
		},
	},
}

var alertPriorities = []string{"low", "medium", "high", "critical"}

func integrationKinds() []string {
	return slices.Sorted(maps.Keys(integrationKindDefinitions))
}

func (setting integrationSetting) attribute() schema.Attribute {
	if setting.isSwitch {
		return schema.BoolAttribute{
			Optional:      true,
			Computed:      true,
			Description:   setting.description,
			PlanModifiers: []planmodifier.Bool{startingValueOfTheKind{}},
		}
	}
	return schema.StringAttribute{
		Optional:    true,
		Sensitive:   setting.secret,
		Description: setting.description,
	}
}

func integrationSettingAttributes() map[string]schema.Attribute {
	attributes := make(map[string]schema.Attribute, len(integrationSettingDefinitions))
	for name, setting := range integrationSettingDefinitions {
		attributes[name] = setting.attribute()
	}
	return attributes
}

func integrationSettingTypes() map[string]attr.Type {
	settingTypes := make(map[string]attr.Type, len(integrationSettingDefinitions))
	for name, setting := range integrationSettingDefinitions {
		settingTypes[name] = setting.attribute().GetType()
	}
	return settingTypes
}

func integrationSettingValue(ctx context.Context, settings types.Object, name string) attr.Value {
	value, declared := settings.Attributes()[name]
	if !declared {
		return integrationSettingDefinitions[name].attribute().GetType().ValueType(ctx)
	}
	return value
}

func integrationSettingIsBlank(value attr.Value) bool {
	text, isText := value.(types.String)
	return value.IsNull() || (isText && !text.IsUnknown() && trimJavaScriptWhitespace(text.ValueString()) == "")
}

func stringSetValue(values []string) types.Set {
	elements := make([]attr.Value, len(values))
	for i, value := range values {
		elements[i] = types.StringValue(value)
	}
	return types.SetValueMust(types.StringType, elements)
}

type startingValueOfTheKind struct{}

func (modifier startingValueOfTheKind) Description(ctx context.Context) string {
	return "left out, it takes the value a new integration of the kind starts with"
}

func (modifier startingValueOfTheKind) MarkdownDescription(ctx context.Context) string {
	return modifier.Description(ctx)
}

func (modifier startingValueOfTheKind) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	planned, diagnostics := plannedValueWhenLeftOut(ctx, req.Path, req.Plan, req.ConfigValue, req.PlanValue, types.BoolNull())
	resp.Diagnostics.Append(diagnostics...)
	resp.PlanValue = planned
}

func (modifier startingValueOfTheKind) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	planned, diagnostics := plannedValueWhenLeftOut(ctx, req.Path, req.Plan, req.ConfigValue, req.PlanValue, types.StringNull())
	resp.Diagnostics.Append(diagnostics...)
	resp.PlanValue = planned
}

func (modifier startingValueOfTheKind) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	planned, diagnostics := plannedValueWhenLeftOut(ctx, req.Path, req.Plan, req.ConfigValue, req.PlanValue, types.SetNull(types.StringType))
	resp.Diagnostics.Append(diagnostics...)
	resp.PlanValue = planned
}

func plannedValueWhenLeftOut[Value attr.Value](ctx context.Context, attributePath path.Path, plan tfsdk.Plan, declared attr.Value, planned, noValue Value) (Value, diag.Diagnostics) {
	if plan.Raw.IsNull() || !declared.IsNull() {
		return planned, nil
	}
	var kind types.String
	diagnostics := plan.GetAttribute(ctx, path.Root("kind"), &kind)
	if diagnostics.HasError() || kind.IsUnknown() {
		return planned, diagnostics
	}

	startingValue, kindStartsWithIt := integrationKindDefinitions[kind.ValueString()].startingValues[attributeNameOf(attributePath)]
	if !kindStartsWithIt {
		return noValue, diagnostics
	}
	typedStartingValue, fitsTheAttribute := startingValue.(Value)
	if !fitsTheAttribute {
		diagnostics.AddAttributeError(attributePath, "Unexpected Starting Value", fmt.Sprintf("A new %s integration starts with %s here, which this attribute cannot hold.", kind.ValueString(), startingValue))
		return planned, diagnostics
	}
	return typedStartingValue, diagnostics
}

func attributeNameOf(attributePath path.Path) string {
	lastStep, _ := attributePath.Steps().LastStep()
	name, _ := lastStep.(path.PathStepAttributeName)
	return string(name)
}

func newIntegrationResource() resource.Resource {
	return &integrationResource{}
}

type integrationResource struct {
	client *client.Client
}

type integrationModel struct {
	Id              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Kind            types.String `tfsdk:"kind"`
	Settings        types.Object `tfsdk:"settings"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	ProjectIds      types.Set    `tfsdk:"project_ids"`
	EnvironmentIds  types.Set    `tfsdk:"environment_ids"`
	TagIds          types.Set    `tfsdk:"tag_ids"`
	EventTypes      types.Set    `tfsdk:"event_types"`
	AlertKinds      types.Set    `tfsdk:"alert_kinds"`
	MinimumPriority types.String `tfsdk:"minimum_priority"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func (r *integrationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *integrationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An integration connects the organization to an analytics tool or to a Slack channel. An analytics integration marks the tool's charts when an experiment's run starts, its split changes, a variation is shipped or the run is stopped. " +
			"A Slack integration posts a message to one channel for each change and alert it selects. An organization can have up to 10 analytics integrations and 20 Slack integrations, several of one kind if it needs them. " +
			"See https://docs.configdirector.com/integrations/analytics for what each analytics tool needs.\n\n" +
			"Requires an API token with the integrations:manage permission. Import with the integration's ID.\n\n" +
			"ConfigDirector never returns a key, token, secret or Slack webhook URL once it is saved, so Terraform notices when you change one in the configuration but not when one is changed outside Terraform. " +
			"If ConfigDirector disables the integration, after three days of failed deliveries or as soon as Slack refuses a message, the next apply enables it again while enabled is true.",
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
				Description: "What the kind needs to reach the tool or the channel. Each setting below names the kinds that use it. Every kind's settings are required, except endpoint_url, message_prefix and post_resolved_alerts, and a setting another kind uses is refused.",
				Attributes:  integrationSettingAttributes(),
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the integration posts to the tool or the channel. Defaults to true. Disabling it abandons the deliveries still waiting for an attempt.",
				Default:     booldefault.StaticBool(true),
			},
			"project_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs of experiments in these projects, or post to Slack only the changes and alerts in these projects, along with alerts that belong to no project. Leave it empty, as it is by default, for every project.",
				Default:     stringSetDefault(nil),
			},
			"environment_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs in these environments, or post to Slack only the changes and alerts in these environments, along with those that belong to no environment. Leave it empty, as it is by default, for every environment.",
				Default:     stringSetDefault(nil),
			},
			"tag_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Mark only the runs of experiments that have one of these tags, or post to Slack only the changes and alerts about configs that have one of them, along with those about no config. Leave it empty, as it is by default, for every config.",
				Default:     stringSetDefault(nil),
			},
			"event_types": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Slack: the changes the integration posts, as webhook event types such as config.updated or targeting_rules.updated. https://docs.configdirector.com/integrations/webhooks lists them. " +
					"Defaults to " + strings.Join(slackStartingEventTypes, ", ") + ". Set it to [] to post only alerts.",
				PlanModifiers: []planmodifier.Set{startingValueOfTheKind{}},
			},
			"alert_kinds": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Slack: the alert kinds whose alerts the integration posts, such as config-not-found. " +
					"Defaults to the application request alerts: " + strings.Join(slackStartingAlertKinds, ", ") + ". Set it to [] to post only changes.",
				PlanModifiers: []planmodifier.Set{startingValueOfTheKind{}},
			},
			"minimum_priority": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Slack: the priority an alert of a selected kind must reach to be posted, one of " + strings.Join(alertPriorities, ", ") + ". An alert raised lower is posted when its priority rises to it. Defaults to " + slackStartingMinimumPriority + ".",
				Validators: []validator.String{
					stringvalidator.OneOf(alertPriorities...),
				},
				PlanModifiers: []planmodifier.String{startingValueOfTheKind{}},
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
	var config integrationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	kind, kindExists := integrationKindDefinitions[config.Kind.ValueString()]
	if !kindExists {
		return
	}

	declaredSelection := map[string]attr.Value{"alert_kinds": config.AlertKinds, "event_types": config.EventTypes, "minimum_priority": config.MinimumPriority}
	for _, name := range slices.Sorted(maps.Keys(declaredSelection)) {
		if !kind.takes(name) && !declaredSelection[name].IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Attribute of another integration kind", fmt.Sprintf("%s does not apply to the %s kind. Remove it.", name, config.Kind.ValueString()))
		}
	}

	if config.Settings.IsNull() || config.Settings.IsUnknown() {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(integrationSettingDefinitions)) {
		setting := integrationSettingDefinitions[name]
		value := integrationSettingValue(ctx, config.Settings, name)
		isRequired := slices.Contains(kind.requiredSettings, name)
		settingPath := path.Root("settings").AtName(name)
		switch {
		case isRequired && integrationSettingIsBlank(value):
			resp.Diagnostics.AddAttributeError(settingPath, "Missing integration setting", setting.sentenceWhenMissing)
		case !kind.takes(name) && !value.IsNull():
			resp.Diagnostics.AddAttributeError(settingPath, "Setting of another integration kind", fmt.Sprintf("%s does not apply to the %s kind. Remove it from settings.", name, config.Kind.ValueString()))
		default:
			continue
		}
	}
}

func (r *integrationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || resp.Plan.Raw.IsNull() || resp.Plan.Raw.Equal(req.State.Raw) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("updated_at"), types.StringUnknown())...)
}

func integrationRequestFrom(ctx context.Context, model integrationModel, sendsCredentials bool) (client.IntegrationRequest, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	settings := map[string]any{}
	for name, setting := range integrationSettingDefinitions {
		value := integrationSettingValue(ctx, model.Settings, name)
		if value.IsNull() || (setting.secret && !sendsCredentials) {
			continue
		}
		sent, err := jsonFromAttrValue(value)
		if err != nil {
			diagnostics.AddAttributeError(path.Root("settings").AtName(name), "Unexpected Integration Setting", err.Error())
			continue
		}
		settings[setting.apiField] = sent
	}

	request := client.IntegrationRequest{
		Name:            model.Name.ValueString(),
		Kind:            model.Kind.ValueString(),
		Settings:        settings,
		Enabled:         model.Enabled.ValueBool(),
		ProjectIDs:      stringsOfSet(ctx, model.ProjectIds, &diagnostics),
		EnvironmentIDs:  stringsOfSet(ctx, model.EnvironmentIds, &diagnostics),
		TagIDs:          stringsOfSet(ctx, model.TagIds, &diagnostics),
		EventTypes:      stringsOfDeclaredSet(ctx, model.EventTypes, &diagnostics),
		AlertKinds:      stringsOfDeclaredSet(ctx, model.AlertKinds, &diagnostics),
		MinimumPriority: stringPtrFromValue(model.MinimumPriority),
	}
	return request, diagnostics
}

func stringsOfDeclaredSet(ctx context.Context, set types.Set, diagnostics *diag.Diagnostics) *[]string {
	if set.IsNull() {
		return nil
	}
	values := stringsOfSet(ctx, set, diagnostics)
	return &values
}

func setOfSavedStrings(ctx context.Context, values *[]string, diagnostics *diag.Diagnostics) types.Set {
	if values == nil {
		return types.SetNull(types.StringType)
	}
	return setOfStrings(ctx, *values, diagnostics)
}

func integrationCredentialsChanged(ctx context.Context, planned, saved types.Object) bool {
	for name, setting := range integrationSettingDefinitions {
		if setting.secret && !integrationSettingValue(ctx, planned, name).Equal(integrationSettingValue(ctx, saved, name)) {
			return true
		}
	}
	return false
}

func savedInstanceURLMatches(declared types.String, saved string) bool {
	return !declared.IsNull() && !declared.IsUnknown() && strings.TrimRight(trimJavaScriptWhitespace(declared.ValueString()), "/") == saved
}

func integrationSettingStateValue(ctx context.Context, setting integrationSetting, declared attr.Value, saved any) (attr.Value, error) {
	if setting.secret {
		return declared, nil
	}
	switch savedValue := saved.(type) {
	case nil:
		return setting.attribute().GetType().ValueType(ctx), nil
	case bool:
		return types.BoolValue(savedValue), nil
	case string:
		declaredText, _ := declared.(types.String)
		if setting.savedWithoutTrailingSlashes && savedInstanceURLMatches(declaredText, savedValue) {
			return declaredText, nil
		}
		return declaredOrSaved(declaredText, savedValue), nil
	default:
		return nil, fmt.Errorf("ConfigDirector returned %v for this setting, which is neither text nor true or false", saved)
	}
}

func integrationSettingsForState(ctx context.Context, integration *client.Integration, declared types.Object) (types.Object, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	values := make(map[string]attr.Value, len(integrationSettingDefinitions))
	for name, setting := range integrationSettingDefinitions {
		value, err := integrationSettingStateValue(ctx, setting, integrationSettingValue(ctx, declared, name), integration.Settings[setting.apiField])
		if err != nil {
			diagnostics.AddAttributeError(path.Root("settings").AtName(name), "Unexpected Integration Setting", err.Error())
			return types.ObjectNull(integrationSettingTypes()), diagnostics
		}
		values[name] = value
	}
	settings, objectDiagnostics := types.ObjectValue(integrationSettingTypes(), values)
	diagnostics.Append(objectDiagnostics...)
	return settings, diagnostics
}

func integrationToModel(ctx context.Context, integration *client.Integration, model *integrationModel) diag.Diagnostics {
	settings, diagnostics := integrationSettingsForState(ctx, integration, model.Settings)

	model.Id = stringValue(integration.ID)
	model.Name = declaredOrSaved(model.Name, integration.Name)
	model.Kind = stringValue(integration.Kind)
	model.Settings = settings
	model.Enabled = boolValue(integration.Enabled)
	model.ProjectIds = setOfStrings(ctx, integration.ProjectIDs, &diagnostics)
	model.EnvironmentIds = setOfStrings(ctx, integration.EnvironmentIDs, &diagnostics)
	model.TagIds = setOfStrings(ctx, integration.TagIDs, &diagnostics)
	model.EventTypes = setOfSavedStrings(ctx, integration.EventTypes, &diagnostics)
	model.AlertKinds = setOfSavedStrings(ctx, integration.AlertKinds, &diagnostics)
	model.MinimumPriority = stringPtrValue(integration.MinimumPriority)
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

	sendsCredentials := integrationCredentialsChanged(ctx, plan.Settings, state.Settings)
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
