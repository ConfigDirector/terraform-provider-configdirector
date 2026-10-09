package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

type configTagAssignments struct {
	ids       *[]string
	namesByID map[string][]string
}

type configTagPrivateState interface {
	GetKey(context.Context, string) ([]byte, diag.Diagnostics)
	SetKey(context.Context, string, []byte) diag.Diagnostics
}

func configTagsForWrite(ctx context.Context, api *client.Client, projectID string, configuration tfsdk.Config, plannedNames types.Set) (configTagAssignments, diag.Diagnostics) {
	var configuredNames types.Set
	diagnostics := configuration.GetAttribute(ctx, path.Root("tags"), &configuredNames)
	if diagnostics.HasError() {
		return configTagAssignments{}, diagnostics
	}
	names := plannedNames
	if configuredNames.IsNull() {
		names = configuredNames
	}
	if names.IsNull() {
		return configTagAssignments{}, diagnostics
	}
	if names.IsUnknown() {
		diagnostics.AddAttributeError(path.Root("tags"), "Unknown Tag Names", "Tag names must be known before assigning tags.")
		return configTagAssignments{}, diagnostics
	}
	var declared []string
	diagnostics.Append(names.ElementsAs(ctx, &declared, false)...)
	if diagnostics.HasError() {
		return configTagAssignments{}, diagnostics
	}
	ids := make([]string, 0, len(declared))
	namesByID := make(map[string][]string, len(declared))
	for _, name := range declared {
		tag, err := api.GetTagByName(ctx, projectID, name)
		if err != nil {
			guidance := ""
			var apiError *client.APIError
			if errors.As(err, &apiError) {
				switch apiError.StatusCode {
				case 404:
					guidance = " Verify the project and literal name, or create the catalog entry with configdirector_tag before assigning it."
				case 403:
					guidance = " Name resolution requires tags:read for this project; config permissions alone do not grant catalog access."
				}
			}
			diagnostics.AddAttributeError(path.Root("tags"), "Error Finding Config Tag", fmt.Sprintf("Cannot assign tag %q in project %q: %s.%s", name, projectID, err, guidance))
			return configTagAssignments{}, diagnostics
		}
		if _, exists := namesByID[tag.ID]; !exists {
			ids = append(ids, tag.ID)
		}
		namesByID[tag.ID] = append(namesByID[tag.ID], name)
	}
	return configTagAssignments{ids: &ids, namesByID: namesByID}, diagnostics
}

func (assignments configTagAssignments) names(ctx context.Context, tags []client.AssignedTag) (types.Set, diag.Diagnostics) {
	assignmentIDsUnchanged := len(tags) == len(assignments.namesByID)
	for _, tag := range tags {
		if _, exists := assignments.namesByID[tag.ID]; !exists {
			assignmentIDsUnchanged = false
			break
		}
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if assignmentIDsUnchanged {
			names = append(names, assignments.namesByID[tag.ID]...)
		} else {
			names = append(names, tag.Name)
		}
	}
	return types.SetValueFrom(ctx, types.StringType, names)
}

func (assignments configTagAssignments) save(ctx context.Context, private configTagPrivateState) diag.Diagnostics {
	if assignments.namesByID == nil {
		return private.SetKey(ctx, "config_tag_names", nil)
	}
	encoded, err := json.Marshal(assignments.namesByID)
	if err != nil {
		return diag.Diagnostics{diag.NewErrorDiagnostic("Error Saving Config Tag Names", err.Error())}
	}
	return private.SetKey(ctx, "config_tag_names", encoded)
}

func (assignments configTagAssignments) creationError(err error) string {
	permissions := "configs:create"
	if assignments.ids != nil && len(*assignments.ids) > 0 {
		permissions += " and config-settings:update"
	}
	return configWriteError(err, permissions)
}

func configWriteError(err error, permissions string) string {
	var apiError *client.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 403 {
		return err.Error()
	}
	return fmt.Sprintf("%s. This operation requires %s for this project.", err, permissions)
}

func configTagsFromPrivateState(ctx context.Context, private configTagPrivateState) (configTagAssignments, diag.Diagnostics) {
	encoded, diagnostics := private.GetKey(ctx, "config_tag_names")
	var assignments configTagAssignments
	if len(encoded) == 0 || diagnostics.HasError() {
		return assignments, diagnostics
	}
	if err := json.Unmarshal(encoded, &assignments.namesByID); err != nil {
		diagnostics.AddError("Error Reading Config Tag Names", err.Error())
	}
	return assignments, diagnostics
}
