package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type configRole struct {
	nameWithArticle string
	decidedLifetime string
	canBecome       string
}

var configRoles = map[string]configRole{
	"config":      {nameWithArticle: "A config"},
	"flag":        {nameWithArticle: "A feature flag", decidedLifetime: "temporary", canBecome: "kill-switch"},
	"kill-switch": {nameWithArticle: "A kill switch", decidedLifetime: "permanent", canBecome: "flag"},
	"experiment":  {nameWithArticle: "An experiment", decidedLifetime: "temporary"},
}

func (role configRole) decidesLifetime() bool {
	return role.decidedLifetime != ""
}

type lifetimeMatchesRole struct{}

func (v lifetimeMatchesRole) Description(ctx context.Context) string {
	return "lifetime must be the one the role decides, except for the \"config\" role"
}

func (v lifetimeMatchesRole) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lifetimeMatchesRole) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var roleName, lifetime types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("role"), &roleName)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("lifetime"), &lifetime)...)
	if resp.Diagnostics.HasError() || roleName.IsUnknown() || lifetime.IsNull() || lifetime.IsUnknown() {
		return
	}

	role := configRoles[roleName.ValueString()]
	if role.decidesLifetime() && lifetime.ValueString() != role.decidedLifetime {
		resp.Diagnostics.AddAttributeError(path.Root("lifetime"), "Invalid lifetime", role.nameWithArticle+" is always "+role.decidedLifetime+".")
	}
}

type roleChangesOnlyBetweenFlagAndKillSwitch struct{}

func (m roleChangesOnlyBetweenFlagAndKillSwitch) Description(ctx context.Context) string {
	return "Only a feature flag can become a kill switch, and a kill switch a feature flag."
}

func (m roleChangesOnlyBetweenFlagAndKillSwitch) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m roleChangesOnlyBetweenFlagAndKillSwitch) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	storedRole, plannedRole := req.StateValue.ValueString(), req.PlanValue.ValueString()
	if plannedRole != storedRole && configRoles[storedRole].canBecome != plannedRole {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid role change", m.Description(ctx))
	}
}

type lifetimeWhenLeftOut struct{}

func (m lifetimeWhenLeftOut) Description(ctx context.Context) string {
	return "When lifetime is left out, plans the lifetime the role decides, or keeps the lifetime of an existing config with the config role."
}

func (m lifetimeWhenLeftOut) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m lifetimeWhenLeftOut) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	var roleName types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("role"), &roleName)...)
	if resp.Diagnostics.HasError() || roleName.IsUnknown() {
		return
	}

	role := configRoles[roleName.ValueString()]
	switch {
	case role.decidesLifetime():
		resp.PlanValue = types.StringValue(role.decidedLifetime)
	case req.State.Raw.IsNull():
		resp.Diagnostics.AddAttributeError(req.Path, "Missing lifetime", `A config needs a lifetime. Set lifetime to "temporary" or "permanent".`)
	default:
		resp.PlanValue = req.StateValue
	}
}
