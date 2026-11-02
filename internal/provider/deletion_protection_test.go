package provider

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const protectedProjectID = "5a1c9e2f-3b4d-4e6f-8a7b-9c0d1e2f3a4b"
const protectedEnvironmentID = "6b2d0f3a-4c5e-4f70-9b8c-0d1e2f3a4b5c"
const protectedProjectPath = "/v1/projects/" + protectedProjectID
const protectedEnvironmentsPath = protectedProjectPath + "/environments"
const protectedEnvironmentPath = protectedEnvironmentsPath + "/" + protectedEnvironmentID

const protectedEnvironmentJSON = `{"id":"` + protectedEnvironmentID + `","slug":"production","name":"Production","color":"red","projectId":"` + protectedProjectID + `","createdAt":"2026-11-01T10:00:00.000Z","updatedAt":"2026-11-01T10:00:00.000Z","live":true,"deletionProtection":true}`
const protectedProjectFields = `"id":"` + protectedProjectID + `","name":"Storefront","slug":"storefront","organizationId":"7c3e1a4b-5d6f-4081-8c9d-1e2f3a4b5c6d","storeCodeSnippets":true,"deletionProtection":true,"createdAt":"2026-11-01T10:00:00.000Z","updatedAt":"2026-11-01T10:00:00.000Z"`

const projectProtectedSentence = "Deletion protection is on for this project. Turn it off in the dashboard under the project's settings before deleting it."
const environmentProtectedSentence = "Deletion protection is on for this environment. Turn it off in the dashboard under the environment's settings before deleting it."

func serveProtectedProject(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v1/projects":
		deletionProtectionStubJSON(response, http.StatusOK, `[{`+protectedProjectFields+`}]`)
	case request.Method == http.MethodGet && request.URL.Path == protectedProjectPath:
		deletionProtectionStubJSON(response, http.StatusOK, `{`+protectedProjectFields+`,"environments":[`+protectedEnvironmentJSON+`]}`)
	case request.Method == http.MethodDelete && request.URL.Path == protectedProjectPath:
		deletionProtectionStubJSON(response, http.StatusBadRequest, `{"errors":[{"message":"`+projectProtectedSentence+`"}]}`)
	case request.Method == http.MethodGet && request.URL.Path == protectedEnvironmentsPath:
		deletionProtectionStubJSON(response, http.StatusOK, `[`+protectedEnvironmentJSON+`]`)
	case request.Method == http.MethodGet && request.URL.Path == protectedEnvironmentPath:
		deletionProtectionStubJSON(response, http.StatusOK, protectedEnvironmentJSON)
	case request.Method == http.MethodDelete && request.URL.Path == protectedEnvironmentPath:
		deletionProtectionStubJSON(response, http.StatusBadRequest, `{"errors":[{"message":"`+environmentProtectedSentence+`"}]}`)
	default:
		deletionProtectionStubJSON(response, http.StatusNotFound, `{}`)
	}
}

func deletionProtectionStubJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write([]byte(body))
}

func deletionProtectionDynamic(t *testing.T, value tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	dynamic, err := tfprotov6.NewDynamicValue(value.Type(), value)
	if err != nil {
		t.Fatal(err)
	}
	return &dynamic
}

func requireDeletionProtectionDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("unexpected diagnostic: %s: %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
}

type deletionProtectionProtocol struct {
	server     tfprotov6.ProviderServer
	definition *tfprotov6.GetProviderSchemaResponse
}

func newDeletionProtectionProtocol(t *testing.T) deletionProtectionProtocol {
	t.Helper()

	api := httptest.NewServer(http.HandlerFunc(serveProtectedProject))
	t.Cleanup(api.Close)
	server := providerserver.NewProtocol6(New("test")())()
	definition, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, definition.Diagnostics)
	configured, err := server.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		Config: deletionProtectionDynamic(t, tftypes.NewValue(definition.Provider.ValueType(), map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, "deletion-protection-test-token"), "base_url": tftypes.NewValue(tftypes.String, api.URL),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, configured.Diagnostics)
	return deletionProtectionProtocol{server: server, definition: definition}
}

func (protocol deletionProtectionProtocol) resourceType(typeName string) tftypes.Object {
	return protocol.definition.ResourceSchemas[typeName].ValueType().(tftypes.Object)
}

func (protocol deletionProtectionProtocol) config(t *testing.T, typeOf tftypes.Object, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	values := make(map[string]tftypes.Value, len(typeOf.AttributeTypes))
	for name, attributeType := range typeOf.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	maps.Copy(values, attributes)
	return deletionProtectionDynamic(t, tftypes.NewValue(typeOf, values))
}

func (protocol deletionProtectionProtocol) values(t *testing.T, typeOf tftypes.Object, dynamic *tfprotov6.DynamicValue) map[string]tftypes.Value {
	t.Helper()

	value, err := dynamic.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]tftypes.Value
	if err := value.As(&values); err != nil {
		t.Fatal(err)
	}
	return values
}

func (protocol deletionProtectionProtocol) importResource(t *testing.T, typeName, id string) *tfprotov6.DynamicValue {
	t.Helper()

	imported, err := protocol.server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{TypeName: typeName, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, imported.Diagnostics)
	if len(imported.ImportedResources) != 1 {
		t.Fatalf("expected one imported %s, got %d", typeName, len(imported.ImportedResources))
	}
	read, err := protocol.server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: typeName, CurrentState: imported.ImportedResources[0].State,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, read.Diagnostics)
	return read.NewState
}

func (protocol deletionProtectionProtocol) plan(t *testing.T, typeName string, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	t.Helper()

	typeOf := protocol.resourceType(typeName)
	proposed := protocol.values(t, typeOf, configuration)
	previous := protocol.values(t, typeOf, prior)
	for _, attribute := range protocol.definition.ResourceSchemas[typeName].Block.Attributes {
		if attribute.Computed && proposed[attribute.Name].IsNull() {
			proposed[attribute.Name] = previous[attribute.Name]
		}
	}
	planned, err := protocol.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: typeName, PriorState: prior, Config: configuration,
		ProposedNewState: deletionProtectionDynamic(t, tftypes.NewValue(typeOf, proposed)),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, planned.Diagnostics)
	return planned
}

func (protocol deletionProtectionProtocol) requireNoChange(t *testing.T, typeName string, prior *tfprotov6.DynamicValue, planned *tfprotov6.PlanResourceChangeResponse) {
	t.Helper()

	typeOf := protocol.resourceType(typeName)
	plannedState, err := planned.PlannedState.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	priorState, err := prior.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	if !plannedState.Equal(priorState) || len(planned.RequiresReplace) > 0 {
		t.Fatalf("expected no change to %s, planned %s from %s", typeName, plannedState, priorState)
	}
}

func (protocol deletionProtectionProtocol) destroyErrorDetail(t *testing.T, typeName string, state *tfprotov6.DynamicValue) string {
	t.Helper()

	absent := deletionProtectionDynamic(t, tftypes.NewValue(protocol.resourceType(typeName), nil))
	applied, err := protocol.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: typeName, PriorState: state, PlannedState: absent, Config: absent,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range applied.Diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			return diagnostic.Detail
		}
	}
	t.Fatalf("expected destroying the protected %s to fail", typeName)
	return ""
}

func (protocol deletionProtectionProtocol) readDataSource(t *testing.T, typeName string, attributes map[string]tftypes.Value) map[string]tftypes.Value {
	t.Helper()

	typeOf := protocol.definition.DataSourceSchemas[typeName].ValueType().(tftypes.Object)
	read, err := protocol.server.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{
		TypeName: typeName, Config: protocol.config(t, typeOf, attributes),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireDeletionProtectionDiagnostics(t, read.Diagnostics)
	return protocol.values(t, typeOf, read.State)
}

func deletionProtectionOf(t *testing.T, attributes map[string]tftypes.Value) bool {
	t.Helper()

	attribute, exists := attributes["deletion_protection"]
	if !exists {
		t.Fatal("expected a deletion_protection attribute")
	}
	if !attribute.IsKnown() || attribute.IsNull() {
		t.Fatalf("expected deletion_protection to be read from the API, got %s", attribute)
	}
	var protected bool
	if err := attribute.As(&protected); err != nil {
		t.Fatal(err)
	}
	return protected
}

func onlyElementAttributes(t *testing.T, set tftypes.Value) map[string]tftypes.Value {
	t.Helper()

	var elements []tftypes.Value
	if err := set.As(&elements); err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 {
		t.Fatalf("expected one element, got %d", len(elements))
	}
	var attributes map[string]tftypes.Value
	if err := elements[0].As(&attributes); err != nil {
		t.Fatal(err)
	}
	return attributes
}

func TestDeletionProtection_importedProjectReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	typeOf := protocol.resourceType("configdirector_project")

	imported := protocol.importResource(t, "configdirector_project", protectedProjectID)

	project := protocol.values(t, typeOf, imported)
	if !deletionProtectionOf(t, project) {
		t.Fatal("expected the imported project to read deletion_protection = true")
	}
	if !deletionProtectionOf(t, onlyElementAttributes(t, project["environments"])) {
		t.Fatal("expected the imported project's Production environment to read deletion_protection = true")
	}
}

func TestDeletionProtection_importedProjectPlansNoChange(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	typeOf := protocol.resourceType("configdirector_project")
	imported := protocol.importResource(t, "configdirector_project", protectedProjectID)

	planned := protocol.plan(t, "configdirector_project", imported, protocol.config(t, typeOf, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Storefront"), "slug": tftypes.NewValue(tftypes.String, "storefront"),
	}))

	protocol.requireNoChange(t, "configdirector_project", imported, planned)
}

func TestDeletionProtection_importedEnvironmentReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	typeOf := protocol.resourceType("configdirector_environment")

	imported := protocol.importResource(t, "configdirector_environment", protectedProjectID+"/production")

	if !deletionProtectionOf(t, protocol.values(t, typeOf, imported)) {
		t.Fatal("expected the imported environment to read deletion_protection = true")
	}
}

func TestDeletionProtection_importedEnvironmentPlansNoChange(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	typeOf := protocol.resourceType("configdirector_environment")
	imported := protocol.importResource(t, "configdirector_environment", protectedProjectID+"/production")

	planned := protocol.plan(t, "configdirector_environment", imported, protocol.config(t, typeOf, map[string]tftypes.Value{
		"project_id": tftypes.NewValue(tftypes.String, protectedProjectID),
		"name":       tftypes.NewValue(tftypes.String, "Production"),
		"slug":       tftypes.NewValue(tftypes.String, "production"),
		"color":      tftypes.NewValue(tftypes.String, "red"),
		"live":       tftypes.NewValue(tftypes.Bool, true),
	}))

	protocol.requireNoChange(t, "configdirector_environment", imported, planned)
}

func TestDeletionProtection_destroyOfAProtectedProjectFailsNamingWhereToTurnItOff(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	imported := protocol.importResource(t, "configdirector_project", protectedProjectID)

	detail := protocol.destroyErrorDetail(t, "configdirector_project", imported)

	if !strings.Contains(detail, projectProtectedSentence) {
		t.Fatalf("expected the destroy error to carry the API's sentence, got %q", detail)
	}
}

func TestDeletionProtection_destroyOfAProtectedEnvironmentFailsNamingWhereToTurnItOff(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)
	imported := protocol.importResource(t, "configdirector_environment", protectedProjectID+"/production")

	detail := protocol.destroyErrorDetail(t, "configdirector_environment", imported)

	if !strings.Contains(detail, environmentProtectedSentence) {
		t.Fatalf("expected the destroy error to carry the API's sentence, got %q", detail)
	}
}

func TestDeletionProtection_projectDataSourceReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)

	project := protocol.readDataSource(t, "configdirector_project", map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, protectedProjectID),
	})

	if !deletionProtectionOf(t, project) {
		t.Fatal("expected the project data source to read deletion_protection = true")
	}
	if !deletionProtectionOf(t, onlyElementAttributes(t, project["environments"])) {
		t.Fatal("expected the project data source's Production environment to read deletion_protection = true")
	}
}

func TestDeletionProtection_projectsDataSourceReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)

	projects := protocol.readDataSource(t, "configdirector_projects", map[string]tftypes.Value{})

	if !deletionProtectionOf(t, onlyElementAttributes(t, projects["projects"])) {
		t.Fatal("expected the projects data source to read deletion_protection = true")
	}
}

func TestDeletionProtection_environmentDataSourceReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)

	environment := protocol.readDataSource(t, "configdirector_environment", map[string]tftypes.Value{
		"id":         tftypes.NewValue(tftypes.String, protectedEnvironmentID),
		"project_id": tftypes.NewValue(tftypes.String, protectedProjectID),
	})

	if !deletionProtectionOf(t, environment) {
		t.Fatal("expected the environment data source to read deletion_protection = true")
	}
}

func TestDeletionProtection_environmentsDataSourceReadsDeletionProtection(t *testing.T) {
	protocol := newDeletionProtectionProtocol(t)

	environments := protocol.readDataSource(t, "configdirector_environments", map[string]tftypes.Value{
		"project_id": tftypes.NewValue(tftypes.String, protectedProjectID),
	})

	if !deletionProtectionOf(t, onlyElementAttributes(t, environments["environments"])) {
		t.Fatal("expected the environments data source to read deletion_protection = true")
	}
}
