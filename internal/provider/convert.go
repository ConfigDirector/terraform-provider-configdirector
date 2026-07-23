package provider

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func jsonUnmarshalString(s string, out any) error {
	return json.Unmarshal([]byte(s), out)
}

func stringValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func stringPtrValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func boolValue(b bool) types.Bool {
	return types.BoolValue(b)
}

func stringPtrFromValue(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}
