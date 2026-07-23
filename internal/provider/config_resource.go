package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/dynamicplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &ConfigResource{}
	_ resource.ResourceWithImportState = &ConfigResource{}
)

func NewConfigResource() resource.Resource {
	return &ConfigResource{}
}

type ConfigResource struct {
	client *client.Client
}

// ConfigResourceModel omits targets: the OpenAPI spec describes it as a
// polymorphic (anyOf/oneOf) union that doesn't map onto a static Terraform
// schema (see the targeting-rules resource discussion). default_value/
// type_options/variations are the same kind of polymorphic union, but each
// is modeled with Terraform's dynamic type (see dynamicFromJSON/
// jsonFromDynamic in convert.go) so they can be written as plain HCL values
// (a string, number, bool, or nested structure, whichever fits) instead of
// requiring a JSON-encoded string. default_value in particular is:
//   - Required, since the API requires it on create
//   - never reconciled against server state: the API never returns it back
//     (see configToModel), so it's write-only from Terraform's perspective
//   - RequiresReplace, since the API has no endpoint to change it in place
type ConfigResourceModel struct {
	Id             types.String  `tfsdk:"id"`
	ProjectId      types.String  `tfsdk:"project_id"`
	Key            types.String  `tfsdk:"key"`
	Description    types.String  `tfsdk:"description"`
	Role           types.String  `tfsdk:"role"`
	Lifetime       types.String  `tfsdk:"lifetime"`
	Type           types.String  `tfsdk:"type"`
	TypeOptions    types.Dynamic `tfsdk:"type_options"`
	State          types.String  `tfsdk:"state"`
	Client         types.Bool    `tfsdk:"client"`
	Server         types.Bool    `tfsdk:"server"`
	Variations     types.Dynamic `tfsdk:"variations"`
	DeprecatedKeys types.List    `tfsdk:"deprecated_keys"`
	DefaultValue   types.Dynamic `tfsdk:"default_value"`
}

func (r *ConfigResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config"
}

func (r *ConfigResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project this config belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 150),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(1000),
				},
			},
			"role": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("config", "flag", "kill-switch", "experiment"),
				},
			},
			"lifetime": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("permanent", "temporary"),
				},
			},
			"type": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("boolean", "string", "integer", "float", "enum", "url", "json", "timespan"),
				},
			},
			"type_options": schema.DynamicAttribute{
				Optional: true,
				Computed: true,
				Description: "Type-specific options for this config, shaped differently depending on \"type\" " +
					"(e.g. min/max for integer/float, values for enum, unit for timespan). Not validated by " +
					"Terraform - the structure you provide is passed through as-is and validated by the API.",
			},
			"state": schema.StringAttribute{
				Computed: true,
			},
			"client": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"server": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			// A list of objects with a per-item dynamic "value" isn't possible
			// here: terraform-plugin-framework doesn't support a dynamic type
			// nested inside a collection type. Instead the whole array is one
			// dynamic value, e.g. variations = [{ name = "on", value = true },
			// { name = "off", value = false }] - see variationsToDynamicJSON/
			// variationsFromDynamic in convert.go.
			"variations": schema.DynamicAttribute{
				Optional: true,
				Computed: true,
				Description: "Per-variation values for this config, as a list of {name, value} objects. Only valid " +
					"when role is \"experiment\"; the API rejects it (and this provider validates it client-side) " +
					"for any other role. Not validated by Terraform beyond that - the structure you provide is " +
					"passed through as-is and validated by the API.",
				Validators: []validator.Dynamic{
					variationsRequireExperimentRole{},
				},
			},
			"deprecated_keys": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"config_id":  schema.StringAttribute{Computed: true},
						"id":         schema.StringAttribute{Computed: true},
						"is_primary": schema.BoolAttribute{Computed: true},
						"key":        schema.StringAttribute{Computed: true},
					},
				},
			},
			"default_value": schema.DynamicAttribute{
				Required: true,
				Description: "Default value for this config - a plain string, number, or bool matching \"type\". " +
					"The ConfigDirector API has no endpoint that returns the global default back, so this value is " +
					"write-only from Terraform's perspective: it is sent on create and never reconciled against server " +
					"state, and cannot be changed in place (any change forces replacement).",
				PlanModifiers: []planmodifier.Dynamic{dynamicplanmodifier.RequiresReplace()},
			},
		},
	}
}

// variationsRequireExperimentRole rejects a non-null "variations" value
// unless the sibling "role" attribute is "experiment". This can't be
// expressed as a per-attribute validator.String on "role" (it needs to see
// both attributes), so it's implemented as a validator.Dynamic on
// "variations" that reads "role" out of the surrounding config instead.
type variationsRequireExperimentRole struct{}

func (v variationsRequireExperimentRole) Description(ctx context.Context) string {
	return "variations can only be set when role is \"experiment\""
}

func (v variationsRequireExperimentRole) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v variationsRequireExperimentRole) ValidateDynamic(ctx context.Context, req validator.DynamicRequest, resp *validator.DynamicResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var role types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("role"), &role)...)
	if resp.Diagnostics.HasError() || role.IsUnknown() {
		return
	}

	if role.ValueString() != "experiment" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid variations",
			"variations can only be set when role is \"experiment\", got role: "+role.ValueString(),
		)
	}
}

func (r *ConfigResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

// configToModel populates every field except DefaultValue, which the API
// never returns; callers must set/preserve it separately.
func configToModel(ctx context.Context, c *client.Config, m *ConfigResourceModel) error {
	m.Id = stringValue(c.ID)
	m.ProjectId = stringValue(c.ProjectID)
	m.Key = stringValue(c.Key)
	m.Description = stringPtrValue(c.Description)
	m.Role = stringValue(c.Role)
	m.Lifetime = stringValue(c.Lifetime)
	m.Type = stringValue(c.Type)
	m.State = stringValue(c.State)
	m.Client = boolValue(c.Client)
	m.Server = boolValue(c.Server)

	typeOptions, err := dynamicFromJSON(c.TypeOptions)
	if err != nil {
		return fmt.Errorf("converting typeOptions: %w", err)
	}
	m.TypeOptions = typeOptions

	variations, err := dynamicFromJSON(variationsToDynamicJSON(c.Variations))
	if err != nil {
		return fmt.Errorf("converting variations: %w", err)
	}
	m.Variations = variations

	keysList, diags := deprecatedKeysListValue(ctx, c.DeprecatedKeys)
	if diags.HasError() {
		return fmt.Errorf("%v", diags)
	}
	m.DeprecatedKeys = keysList
	return nil
}

func (r *ConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	defaultVal, err := jsonFromDynamic(plan.DefaultValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("default_value"), "Invalid default_value", err.Error())
		return
	}

	typeOptions, err := jsonFromDynamic(plan.TypeOptions)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("type_options"), "Invalid type_options", err.Error())
		return
	}

	variations, err := variationsFromDynamic(plan.Variations)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("variations"), "Invalid variations", err.Error())
		return
	}

	server := plan.Server.ValueBool()
	clientFlag := plan.Client.ValueBool()
	cfg, err := r.client.CreateConfig(ctx, plan.ProjectId.ValueString(), client.CreateConfigRequest{
		Key:          plan.Key.ValueString(),
		Description:  stringPtrFromValue(plan.Description),
		Role:         plan.Role.ValueString(),
		Lifetime:     plan.Lifetime.ValueString(),
		Type:         plan.Type.ValueString(),
		TypeOptions:  typeOptions,
		Server:       &server,
		Client:       &clientFlag,
		Variations:   variations,
		DefaultValue: defaultVal,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &plan); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.GetConfig(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &state); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typeOptions, err := jsonFromDynamic(plan.TypeOptions)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("type_options"), "Invalid type_options", err.Error())
		return
	}

	variations, err := variationsFromDynamic(plan.Variations)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("variations"), "Invalid variations", err.Error())
		return
	}

	cfg, err := r.client.UpdateConfig(ctx, plan.ProjectId.ValueString(), state.Key.ValueString(), client.UpdateConfigRequest{
		Key:         plan.Key.ValueString(),
		Description: stringPtrFromValue(plan.Description),
		Role:        plan.Role.ValueString(),
		Lifetime:    plan.Lifetime.ValueString(),
		Type:        plan.Type.ValueString(),
		TypeOptions: typeOptions,
		Variations:  variations,
		Server:      plan.Server.ValueBool(),
		Client:      plan.Client.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Config", err.Error())
		return
	}

	if err := configToModel(ctx, cfg, &plan); err != nil {
		resp.Diagnostics.AddError("Error Processing Config Response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteConfig(ctx, state.ProjectId.ValueString(), state.Key.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Config", err.Error())
	}
}

func (r *ConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id/key. Got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[1])...)
	resp.Diagnostics.AddWarning(
		"default_value Not Imported",
		"The ConfigDirector API does not return a config's default value, so default_value cannot be populated by import. Set it manually after import or the next plan will show a replacement.",
	)
}
