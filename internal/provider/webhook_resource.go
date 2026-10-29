package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &webhookResource{}
	_ resource.ResourceWithImportState = &webhookResource{}
)

var httpHeaderNamePattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

var everyChangeEventType = []string{
	"config.created",
	"config.updated",
	"config.archived",
	"config.restored",
	"targeting_rules.updated",
	"segment.created",
	"segment.updated",
	"segment.deleted",
	"environment.created",
	"environment.updated",
	"environment.deleted",
	"project.created",
	"project.updated",
	"project.deleted",
}

func stringSetDefault(values []string) defaults.Set {
	elements := make([]attr.Value, len(values))
	for i, value := range values {
		elements[i] = types.StringValue(value)
	}
	return setdefault.StaticValue(types.SetValueMust(types.StringType, elements))
}

func newWebhookResource() resource.Resource {
	return &webhookResource{}
}

type webhookResource struct {
	client *client.Client
}

type webhookModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Url            types.String `tfsdk:"url"`
	Headers        types.Map    `tfsdk:"headers"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	ProjectIds     types.Set    `tfsdk:"project_ids"`
	EnvironmentIds types.Set    `tfsdk:"environment_ids"`
	TagIds         types.Set    `tfsdk:"tag_ids"`
	EventTypes     types.Set    `tfsdk:"event_types"`
	AlertKinds     types.Set    `tfsdk:"alert_kinds"`
	Secret         types.String `tfsdk:"secret"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (r *webhookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A webhook sends a signed HTTPS request to a URL whenever a config, its targeting rules, a segment, an environment or a project changes, or an alert is raised or resolved. Webhooks belong to the organization, which can have up to 10. " +
			"See https://docs.configdirector.com/integrations/webhooks for what each request holds and how to verify it.\n\n" +
			"Requires an API token with the webhooks:manage permission. Import with the webhook's ID.\n\n" +
			"ConfigDirector never returns header values, so Terraform notices a header added or removed outside Terraform but not a value changed there. After an import, the first apply writes the header values from the configuration. " +
			"If ConfigDirector disables the webhook after three days of failed deliveries, the next apply enables it again while enabled is true.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The webhook's ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The webhook's name, up to 200 characters.",
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "The URL the webhook sends requests to. It must start with https:// and its host must resolve to a public address. Changing the URL keeps the webhook and its secret.",
			},
			"headers": schema.MapAttribute{
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "Up to 5 custom headers sent with every request, from header name to value, for example an Authorization header your receiver checks. Each name can be used once, whatever its casing. A value is at most 1,024 characters of printable Latin-1 text with no line breaks. " +
					"ConfigDirector sets content-type, content-length, host, user-agent, webhook-id, webhook-signature and webhook-timestamp itself, so they can't be custom headers. " +
					"ConfigDirector never shows a value again once it is saved, so a value changed outside Terraform goes unnoticed.",
				Validators: []validator.Map{
					mapvalidator.KeysAre(stringvalidator.RegexMatches(httpHeaderNamePattern, "must be a header name made of the characters HTTP allows")),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the webhook sends requests. Defaults to true. Disabling it abandons the deliveries still waiting for an attempt.",
				Default:     booldefault.StaticBool(true),
			},
			"project_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Send only the webhook events that belong to these projects. Leave it empty, as it is by default, to receive webhook events from every project. A webhook filtered to projects receives no alert events, because alerts belong to no project.",
				Default:     stringSetDefault(nil),
			},
			"environment_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Send only the webhook events that belong to these environments, along with every webhook event that belongs to no environment: config settings, segments (except a change to one environment's override), projects and alerts. Leave it empty, as it is by default, to receive webhook events from every environment.",
				Default:     stringSetDefault(nil),
			},
			"tag_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Send only the webhook events about configs that have one of these tags, along with every webhook event that isn't about a config: segments, environments, projects and alerts. Leave it empty, as it is by default, to receive webhook events about every config.",
				Default:     stringSetDefault(nil),
			},
			"event_types": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "The change events the webhook receives: " + strings.Join(everyChangeEventType, ", ") + ". " +
					"Defaults to all of them. Set it to [] for a webhook that receives only alert events.",
				Default: stringSetDefault(everyChangeEventType),
			},
			"alert_kinds": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "The alert kinds whose alert events the webhook receives, such as default-value-mismatch. Defaults to none.",
				Default:     stringSetDefault(nil),
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The signing secret, starting with whsec_. Pass it to your receiver so it can verify each request's signature. It stays the same across updates; after a rotation in the dashboard or through the Admin API, the next refresh picks up the new secret.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "When the webhook was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the webhook was last changed.",
			},
		},
	}
}

func (r *webhookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func headerValuesOf(ctx context.Context, headers types.Map, diagnostics *diag.Diagnostics) map[string]string {
	values := map[string]string{}
	if !headers.IsNull() {
		diagnostics.Append(headers.ElementsAs(ctx, &values, false)...)
	}
	return values
}

func webhookRequestFrom(ctx context.Context, model webhookModel) (client.WebhookRequest, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	headerValues := headerValuesOf(ctx, model.Headers, &diagnostics)
	headers := make([]client.WebhookHeader, 0, len(headerValues))
	for _, name := range slices.Sorted(maps.Keys(headerValues)) {
		headers = append(headers, client.WebhookHeader{Name: name, Value: headerValues[name]})
	}

	request := client.WebhookRequest{
		Name:           model.Name.ValueString(),
		URL:            model.Url.ValueString(),
		Headers:        headers,
		Enabled:        model.Enabled.ValueBool(),
		ProjectIDs:     stringsOfSet(ctx, model.ProjectIds, &diagnostics),
		EnvironmentIDs: stringsOfSet(ctx, model.EnvironmentIds, &diagnostics),
		TagIDs:         stringsOfSet(ctx, model.TagIds, &diagnostics),
		EventTypes:     stringsOfSet(ctx, model.EventTypes, &diagnostics),
		AlertKinds:     stringsOfSet(ctx, model.AlertKinds, &diagnostics),
	}
	return request, diagnostics
}

func stringsOfSet(ctx context.Context, set types.Set, diagnostics *diag.Diagnostics) []string {
	values := make([]string, 0, len(set.Elements()))
	diagnostics.Append(set.ElementsAs(ctx, &values, false)...)
	return values
}

func savedHeadersWithDeclaredValues(ctx context.Context, declared types.Map, saved []client.WebhookHeaderName) (types.Map, diag.Diagnostics) {
	if declared.IsNull() && len(saved) == 0 {
		return types.MapNull(types.StringType), nil
	}

	var diagnostics diag.Diagnostics
	declaredValues := headerValuesOf(ctx, declared, &diagnostics)
	values := make(map[string]string, len(saved))
	for _, header := range saved {
		values[header.Name] = declaredValues[header.Name]
	}
	headers, mapDiagnostics := types.MapValueFrom(ctx, types.StringType, values)
	diagnostics.Append(mapDiagnostics...)
	return headers, diagnostics
}

func webhookToModel(ctx context.Context, webhook *client.Webhook, model *webhookModel) diag.Diagnostics {
	headers, diagnostics := savedHeadersWithDeclaredValues(ctx, model.Headers, webhook.Headers)

	model.Id = stringValue(webhook.ID)
	model.Name = declaredOrSaved(model.Name, webhook.Name)
	model.Url = declaredOrSaved(model.Url, webhook.URL)
	model.Headers = headers
	model.Enabled = boolValue(webhook.Enabled)
	model.ProjectIds = setOfStrings(ctx, webhook.ProjectIDs, &diagnostics)
	model.EnvironmentIds = setOfStrings(ctx, webhook.EnvironmentIDs, &diagnostics)
	model.TagIds = setOfStrings(ctx, webhook.TagIDs, &diagnostics)
	model.EventTypes = setOfStrings(ctx, webhook.EventTypes, &diagnostics)
	model.AlertKinds = setOfStrings(ctx, webhook.AlertKinds, &diagnostics)
	model.Secret = stringValue(webhook.Secret)
	model.CreatedAt = stringValue(webhook.CreatedAt)
	model.UpdatedAt = stringValue(webhook.UpdatedAt)
	return diagnostics
}

func setOfStrings(ctx context.Context, values []string, diagnostics *diag.Diagnostics) types.Set {
	set, setDiagnostics := types.SetValueFrom(ctx, types.StringType, values)
	diagnostics.Append(setDiagnostics...)
	return set
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, diagnostics := webhookRequestFrom(ctx, plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.client.CreateWebhook(ctx, request)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(webhookToModel(ctx, webhook, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.client.GetWebhook(ctx, state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(webhookToModel(ctx, webhook, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, diagnostics := webhookRequestFrom(ctx, plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.client.UpdateWebhook(ctx, state.Id.ValueString(), request)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(webhookToModel(ctx, webhook, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWebhook(ctx, state.Id.ValueString())
	if err != nil {
		var apiError *client.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Webhook", err.Error())
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
