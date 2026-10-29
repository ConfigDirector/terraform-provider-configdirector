package provider

import (
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func trimJavaScriptWhitespace(text string) string {
	return strings.TrimFunc(text, func(character rune) bool {
		return character == '\ufeff' || (character != '\u0085' && unicode.IsSpace(character))
	})
}

func declaredOrSaved(declared types.String, saved string) types.String {
	if !declared.IsNull() && !declared.IsUnknown() && trimJavaScriptWhitespace(declared.ValueString()) == saved {
		return declared
	}
	return stringValue(saved)
}
