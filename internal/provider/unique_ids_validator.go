package provider

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type uniqueIDs struct {
	description string
	summary     string
	detail      string
}

func (v uniqueIDs) Description(ctx context.Context) string {
	return v.description
}

func (v uniqueIDs) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v uniqueIDs) ValidateDynamic(ctx context.Context, req validator.DynamicRequest, resp *validator.DynamicResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw, err := jsonFromDynamic(req.ConfigValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+req.Path.String(), err.Error())
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

	resp.Diagnostics.AddAttributeError(req.Path, v.summary, v.detail+" Duplicated id(s): "+strings.Join(duplicates, ", "))
}

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
