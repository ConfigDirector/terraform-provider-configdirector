package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

var (
	_ resource.Resource                = &ConfigTargetingRulesResource{}
	_ resource.ResourceWithImportState = &ConfigTargetingRulesResource{}
)

func NewConfigTargetingRulesResource() resource.Resource {
	return &ConfigTargetingRulesResource{}
}

type ConfigTargetingRulesResource struct {
	client *client.Client
}

// ConfigTargetingRulesModel represents one config's targeting state (default
// value + rules) for one environment. There's no dedicated GET for this -
// Read fetches the parent config and looks up the matching entry in its
// "targets" by environment id. Rules is a deeply-nested discriminated union
// (conditional-vs-percentage rules, each with their own condition/bucket
// shapes and a 5-variant operator enum) that doesn't map onto a static
// Terraform schema any more than config's typeOptions/variations do, so it's
// modeled as a single dynamic value (see dynamicFromJSON/jsonFromDynamic in
// convert.go), letting the user write whichever shape matches and letting
// the API validate it.
type ConfigTargetingRulesModel struct {
	ProjectId       types.String  `tfsdk:"project_id"`
	ConfigKey       types.String  `tfsdk:"config_key"`
	EnvironmentId   types.String  `tfsdk:"environment_id"`
	EnvironmentSlug types.String  `tfsdk:"environment_slug"`
	DefaultValue    types.String  `tfsdk:"default_value"`
	Rules           types.Dynamic `tfsdk:"rules"`
}

func (r *ConfigTargetingRulesResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config_targeting_rules"
}

func (r *ConfigTargetingRulesResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the project this config belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"config_key": schema.StringAttribute{
				Required:      true,
				Description:   "Key of the config to set targeting rules for.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			// Exactly one of environment_id/environment_slug must be given by
			// the user; whichever is omitted gets filled in from the other by
			// this provider (the API's own environment object embeds both).
			"environment_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("environment_id"),
						path.MatchRoot("environment_slug"),
					),
				},
				// UseStateForUnknown matters here, not just RequiresReplace:
				// without it, this Computed attribute goes unknown on every
				// update that touches anything else (standard behavior for
				// Computed attributes the config leaves unset), which
				// RequiresReplace would then misread as a real change and
				// force replacement on every unrelated update.
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
			"environment_slug": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
			"default_value": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Default value for this environment. Regardless of what type you write here or send " +
					"on write, the API always stores and returns this as a string (confirmed empirically: sending " +
					"a bool or number back gets you a stringified version on read), so it's modeled as a plain " +
					"string rather than a dynamic value. This is a full-replace endpoint: omitting this while " +
					"setting \"rules\" clears any previously configured default value, it does not leave it untouched.",
			},
			// Optional, not Computed: the API adds fields to every rule
			// (valueId, an always-present but often-empty percentages array,
			// ...) beyond whatever was written, so a value read back from
			// the API never structurally matches what was configured -
			// reconciling it the way most Computed attributes do would
			// always trip Terraform's consistency check. So, like
			// configdirector_config's initial_value, this is write-only:
			// sent on create/update, never read back from the API.
			"rules": schema.DynamicAttribute{
				Optional: true,
				Description: "Targeting rules for this environment, as a list of rule objects (conditional or " +
					"percentage-based, matching the API's shape). Not validated by Terraform beyond structure - " +
					"passed through as-is and validated by the API. Every rule/condition/percentage-bucket needs " +
					"an \"id\" (the API requires it, and Terraform has no way for this provider to generate one " +
					"itself - see RuleIDFunction/rule_id_function.go for why): use the " +
					"provider::configdirector::rule_id(\"some-stable-name\") function rather than typing a UUID " +
					"by hand, and give each rule/condition/percentage-bucket its own distinct seed - ids must be " +
					"unique across the entire value (rule ids, condition ids, and percentage-bucket ids all share " +
					"one namespace, including across different rules). Write-only: the API embeds extra generated " +
					"fields (e.g. valueId) into whatever you write, so unlike most attributes this is never " +
					"reconciled against a subsequent read - external changes to targeting rules won't show up as " +
					"drift.",
				Validators: []validator.Dynamic{rulesUniqueIDs{}},
			},
		},
	}
}

// rulesUniqueIDs rejects a "rules" value containing a duplicate "id" -
// whether on two rules, two conditions, two percentage-buckets, or any
// mixture of those, including across different rules. The ids appear to
// share one namespace server-side (rule/condition/percentage-bucket ids all
// show up alongside generated cross-reference fields like valueId), so a
// duplicate anywhere in the tree is treated as a collision, not just within
// its own immediate list.
type rulesUniqueIDs struct{}

func (v rulesUniqueIDs) Description(ctx context.Context) string {
	return "Ensures every rule/condition/percentage-bucket id in this value is unique."
}

func (v rulesUniqueIDs) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v rulesUniqueIDs) ValidateDynamic(ctx context.Context, req validator.DynamicRequest, resp *validator.DynamicResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw, err := jsonFromDynamic(req.ConfigValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid rules", err.Error())
		return
	}

	counts := map[string]int{}
	collectIDs(raw, counts)

	var duplicates []string
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, id)
		}
	}
	if len(duplicates) == 0 {
		return
	}
	sort.Strings(duplicates)

	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Duplicate rule/condition/percentage-bucket id",
		"Each id must be unique across the whole rules value - rule ids, condition ids, and percentage-bucket "+
			"ids all share one namespace, including across different rules. Give each entry its own seed for "+
			"provider::configdirector::rule_id(...). Duplicated id(s): "+strings.Join(duplicates, ", "),
	)
}

// collectIDs recursively tallies every "id" string value found anywhere in a
// JSON-like tree (as produced by jsonFromDynamic).
func collectIDs(v any, counts map[string]int) {
	switch x := v.(type) {
	case map[string]any:
		if id, ok := x["id"].(string); ok && id != "" {
			counts[id]++
		}
		for _, val := range x {
			collectIDs(val, counts)
		}
	case []any:
		for _, item := range x {
			collectIDs(item, counts)
		}
	}
}

func (r *ConfigTargetingRulesResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// resolveEnvironment returns the environment id and slug to use, given
// whichever of the two the user supplied (validator.ExactlyOneOf on the
// schema guarantees exactly one is set). There's no get-environment-by-slug
// endpoint, so a slug is resolved by listing the project's environments.
func resolveEnvironment(ctx context.Context, c *client.Client, projectID string, environmentID, environmentSlug types.String) (id string, slug string, err error) {
	if !environmentID.IsNull() && environmentID.ValueString() != "" {
		envs, err := c.ListEnvironments(ctx, projectID)
		if err != nil {
			return "", "", err
		}
		for _, e := range envs {
			if e.ID == environmentID.ValueString() {
				return e.ID, e.Slug, nil
			}
		}
		return "", "", fmt.Errorf("no environment with id %q was found in project %q", environmentID.ValueString(), projectID)
	}

	envs, err := c.ListEnvironments(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	for _, e := range envs {
		if e.Slug == environmentSlug.ValueString() {
			return e.ID, e.Slug, nil
		}
	}
	return "", "", fmt.Errorf("no environment with slug %q was found in project %q", environmentSlug.ValueString(), projectID)
}

// targetingRulesToModel finds the entry in cfg.Targets for environmentID and
// populates m from it (except Rules, which is write-only - see the schema).
// It returns false if no matching entry was found (the environment doesn't
// exist, or was removed).
func targetingRulesToModel(cfg *client.Config, environmentID string, m *ConfigTargetingRulesModel) bool {
	for _, t := range cfg.Targets {
		if t.Environment.ID != environmentID {
			continue
		}
		m.EnvironmentId = stringValue(t.Environment.ID)
		m.EnvironmentSlug = stringValue(t.Environment.Slug)
		m.DefaultValue = stringPtrValue(t.DefaultValue)
		return true
	}
	return false
}

func (r *ConfigTargetingRulesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConfigTargetingRulesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environmentID, _, err := resolveEnvironment(ctx, r.client, plan.ProjectId.ValueString(), plan.EnvironmentId, plan.EnvironmentSlug)
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Environment", err.Error())
		return
	}

	defaultVal := stringPtrFromValue(plan.DefaultValue)

	rulesVal, err := jsonFromDynamic(plan.Rules)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid rules", err.Error())
		return
	}
	if rulesVal == nil {
		rulesVal = []any{}
	}

	err = r.client.UpdateConfigTargets(ctx, plan.ProjectId.ValueString(), plan.ConfigKey.ValueString(), client.UpdateConfigTargetsRequest{
		EnvironmentID: environmentID,
		DefaultValue:  defaultVal,
		Rules:         rulesVal,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Setting Targeting Rules", err.Error())
		return
	}

	cfg, err := r.client.GetConfig(ctx, plan.ProjectId.ValueString(), plan.ConfigKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}
	if !targetingRulesToModel(cfg, environmentID, &plan) {
		resp.Diagnostics.AddError("Environment Not Found", fmt.Sprintf("Config %q has no targets entry for environment %q after writing targeting rules.", plan.ConfigKey.ValueString(), environmentID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConfigTargetingRulesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConfigTargetingRulesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.GetConfig(ctx, state.ProjectId.ValueString(), state.ConfigKey.ValueString())
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}

	if !targetingRulesToModel(cfg, state.EnvironmentId.ValueString(), &state) {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConfigTargetingRulesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ConfigTargetingRulesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environmentID, _, err := resolveEnvironment(ctx, r.client, plan.ProjectId.ValueString(), plan.EnvironmentId, plan.EnvironmentSlug)
	if err != nil {
		resp.Diagnostics.AddError("Error Resolving Environment", err.Error())
		return
	}

	defaultVal := stringPtrFromValue(plan.DefaultValue)

	rulesVal, err := jsonFromDynamic(plan.Rules)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid rules", err.Error())
		return
	}
	if rulesVal == nil {
		rulesVal = []any{}
	}

	err = r.client.UpdateConfigTargets(ctx, plan.ProjectId.ValueString(), plan.ConfigKey.ValueString(), client.UpdateConfigTargetsRequest{
		EnvironmentID: environmentID,
		DefaultValue:  defaultVal,
		Rules:         rulesVal,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Setting Targeting Rules", err.Error())
		return
	}

	cfg, err := r.client.GetConfig(ctx, plan.ProjectId.ValueString(), plan.ConfigKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Config", err.Error())
		return
	}
	if !targetingRulesToModel(cfg, environmentID, &plan) {
		resp.Diagnostics.AddError("Environment Not Found", fmt.Sprintf("Config %q has no targets entry for environment %q after writing targeting rules.", plan.ConfigKey.ValueString(), environmentID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears this environment's rules rather than truly deleting
// anything: there's no delete endpoint, and PUT is a full replace, so
// writing an empty rules array is the closest equivalent to "this resource
// no longer manages anything here." default_value is sent back unchanged
// (not cleared): despite the OpenAPI schema allowing null, the API actually
// rejects a PUT with defaultValue: null ("Default value is required"), so
// there's no way to clear it - only to replace it with a real value, which
// isn't this resource's call to make on the way out.
func (r *ConfigTargetingRulesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConfigTargetingRulesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateConfigTargets(ctx, state.ProjectId.ValueString(), state.ConfigKey.ValueString(), client.UpdateConfigTargetsRequest{
		EnvironmentID: state.EnvironmentId.ValueString(),
		DefaultValue:  stringPtrFromValue(state.DefaultValue),
		Rules:         []any{},
	})
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error Resetting Targeting Rules", err.Error())
	}
}

// ImportState accepts project_id/config_key/environment_id_or_slug.
func (r *ConfigTargetingRulesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: project_id/config_key/environment_id_or_slug. Got: %q", req.ID),
		)
		return
	}
	projectID, configKey, envIdentifier := parts[0], parts[1], parts[2]

	environmentID, _, err := resolveEnvironment(ctx, r.client, projectID, types.StringNull(), types.StringValue(envIdentifier))
	if err != nil {
		// Fall back to treating the identifier as an id rather than a slug.
		environmentID, _, err = resolveEnvironment(ctx, r.client, projectID, types.StringValue(envIdentifier), types.StringNull())
		if err != nil {
			resp.Diagnostics.AddError("Error Resolving Environment", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("config_key"), configKey)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), environmentID)...)
}
