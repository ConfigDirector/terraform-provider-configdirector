package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type resourceProtocol struct {
	typeName string
	server   tfprotov6.ProviderServer
	schema   *tfprotov6.Schema
	typeOf   tftypes.Object
}

func newResourceProtocol(t *testing.T, typeName, baseURL, token string) resourceProtocol {
	t.Helper()

	server := providerserver.NewProtocol6(New("test")())()
	definition, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, definition.Diagnostics)
	resourceSchema, registered := definition.ResourceSchemas[typeName]
	if !registered {
		t.Fatalf("%s must be registered as a provider resource", typeName)
	}
	configured, err := server.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		Config: dynamicValueOf(t, tftypes.NewValue(definition.Provider.ValueType(), map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, token), "base_url": tftypes.NewValue(tftypes.String, baseURL),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, configured.Diagnostics)
	return resourceProtocol{typeName: typeName, server: server, schema: resourceSchema, typeOf: resourceSchema.ValueType().(tftypes.Object)}
}

func requireNoErrorDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("unexpected diagnostic: %s: %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
}

func firstErrorAttribute(t *testing.T, diagnostics []*tfprotov6.Diagnostic) string {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError && diagnostic.Attribute != nil {
			return fmt.Sprint(diagnostic.Attribute.Steps()[0])
		}
	}
	t.Fatalf("expected an error on an attribute, got %v", diagnostics)
	return ""
}

func dynamicValueOf(t *testing.T, value tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	dynamic, err := tfprotov6.NewDynamicValue(value.Type(), value)
	if err != nil {
		t.Fatal(err)
	}
	return &dynamic
}

func stringSetOf(values ...string) tftypes.Value {
	elements := make([]tftypes.Value, len(values))
	for i, value := range values {
		elements[i] = tftypes.NewValue(tftypes.String, value)
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elements)
}

func (protocol resourceProtocol) attribute(t *testing.T, name string) *tfprotov6.SchemaAttribute {
	t.Helper()

	for _, attribute := range protocol.schema.Block.Attributes {
		if attribute.Name == name {
			return attribute
		}
	}
	t.Fatalf("%s must have a %s attribute", protocol.typeName, name)
	return nil
}

func (protocol resourceProtocol) config(t *testing.T, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	values := make(map[string]tftypes.Value, len(protocol.typeOf.AttributeTypes))
	for name, typeOf := range protocol.typeOf.AttributeTypes {
		values[name] = tftypes.NewValue(typeOf, nil)
	}
	maps.Copy(values, attributes)
	return dynamicValueOf(t, tftypes.NewValue(protocol.typeOf, values))
}

func (protocol resourceProtocol) values(t *testing.T, dynamic *tfprotov6.DynamicValue) map[string]tftypes.Value {
	t.Helper()

	value, err := dynamic.Unmarshal(protocol.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	if value.IsNull() {
		return nil
	}
	var values map[string]tftypes.Value
	if err := value.As(&values); err != nil {
		t.Fatal(err)
	}
	return values
}

func (protocol resourceProtocol) absent(t *testing.T) *tfprotov6.DynamicValue {
	t.Helper()

	return dynamicValueOf(t, tftypes.NewValue(protocol.typeOf, nil))
}

func (protocol resourceProtocol) proposedNewState(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	proposed := protocol.values(t, configuration)
	previous := protocol.values(t, prior)
	for _, attribute := range protocol.schema.Block.Attributes {
		if previous != nil && attribute.Computed && proposed[attribute.Name].IsNull() {
			proposed[attribute.Name] = previous[attribute.Name]
		}
	}
	return dynamicValueOf(t, tftypes.NewValue(protocol.typeOf, proposed))
}

func (protocol resourceProtocol) plan(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	t.Helper()

	planned, err := protocol.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: protocol.typeName, PriorState: prior, Config: configuration,
		ProposedNewState: protocol.proposedNewState(t, prior, configuration),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, planned.Diagnostics)
	return planned
}

func (protocol resourceProtocol) plannedValues(t *testing.T, prior, configuration *tfprotov6.DynamicValue) map[string]tftypes.Value {
	t.Helper()

	return protocol.values(t, protocol.plan(t, prior, configuration).PlannedState)
}

func (protocol resourceProtocol) apply(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	planned := protocol.plan(t, prior, configuration)
	applied, err := protocol.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: protocol.typeName, PriorState: prior, Config: configuration,
		PlannedState: planned.PlannedState, PlannedPrivate: planned.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, applied.Diagnostics)
	return applied.NewState
}

func (protocol resourceProtocol) refresh(t *testing.T, state *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	read, err := protocol.server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: protocol.typeName, CurrentState: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, read.Diagnostics)
	return read.NewState
}

func (protocol resourceProtocol) importByID(t *testing.T, id string) *tfprotov6.DynamicValue {
	t.Helper()

	imported, err := protocol.server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{
		TypeName: protocol.typeName, ID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiagnostics(t, imported.Diagnostics)
	if len(imported.ImportedResources) != 1 {
		t.Fatalf("expected one imported %s, got %d", protocol.typeName, len(imported.ImportedResources))
	}
	return protocol.refresh(t, imported.ImportedResources[0].State)
}

func (protocol resourceProtocol) destroy(t *testing.T, state *tfprotov6.DynamicValue) []*tfprotov6.Diagnostic {
	t.Helper()

	applied, err := protocol.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: protocol.typeName, PriorState: state, PlannedState: protocol.absent(t), Config: protocol.absent(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return applied.Diagnostics
}

func requireKnownValue(t *testing.T, value tftypes.Value) {
	t.Helper()

	if !value.IsKnown() {
		t.Fatal("expected a known value, got one left unknown until apply")
	}
}

func sortedStringsOf(t *testing.T, value tftypes.Value) []string {
	t.Helper()

	requireKnownValue(t, value)
	var elements []tftypes.Value
	if err := value.As(&elements); err != nil {
		t.Fatal(err)
	}
	texts := make([]string, len(elements))
	for i, element := range elements {
		if err := element.As(&texts[i]); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(texts)
	return texts
}

func textOf(t *testing.T, value tftypes.Value) string {
	t.Helper()

	requireKnownValue(t, value)
	var text string
	if err := value.As(&text); err != nil {
		t.Fatal(err)
	}
	return text
}

func booleanOf(t *testing.T, value tftypes.Value) bool {
	t.Helper()

	requireKnownValue(t, value)
	var boolean bool
	if err := value.As(&boolean); err != nil {
		t.Fatal(err)
	}
	return boolean
}

func respondWithJSON(response http.ResponseWriter, status int, body map[string]any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
