package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const configTagsHTTPProjectID = "01234567-89ab-4cde-8f01-23456789abcd"
const configTagsHTTPEndpoint = "/v1/projects/" + configTagsHTTPProjectID + "/configs"
const configTagsHTTPLookup = "/v1/projects/" + configTagsHTTPProjectID + "/tags/by-name"
const configTagsHTTPAssignedConfig = `{"id":"12345678-9abc-4def-8012-3456789abcde","projectId":"01234567-89ab-4cde-8f01-23456789abcd","key":"checkout-flag","role":"flag","lifetime":"temporary","type":"boolean","state":"new","client":true,"server":true,"deprecatedKeys":[],"tags":[{"id":"23456789-abcd-4ef0-8123-456789abcdef","name":"Payments"}]}`
const configTagsHTTPTag = `{"id":"23456789-abcd-4ef0-8123-456789abcdef","projectId":"01234567-89ab-4cde-8f01-23456789abcd","name":"Payments"}`

func newConfigTagsHTTPTest(t *testing.T, handler http.HandlerFunc) configTagsTest {
	t.Helper()

	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	server, definition := configTagsServer(t)
	configured, err := server.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		Config: configTagsDynamic(t, tftypes.NewValue(definition.Provider.ValueType(), map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, "permission-test-token"), "base_url": tftypes.NewValue(tftypes.String, api.URL),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, configured.Diagnostics)
	return configTagsTest{
		server: server, typeOf: definition.ResourceSchemas["configdirector_config"].ValueType(),
		tagTestResource: tagTestResource{project: &client.Project{ID: configTagsHTTPProjectID}},
	}
}

func configTagsHTTPJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (fixture configTagsTest) rejectedChange(t *testing.T, prior configTagsState, configuration *tfprotov6.DynamicValue) []*tfprotov6.Diagnostic {
	t.Helper()

	plan := fixture.plan(t, prior, configuration)
	applied, err := fixture.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_config", PriorState: prior.value, Config: configuration,
		PlannedState: plan.PlannedState, PlannedPrivate: plan.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return applied.Diagnostics
}

func configTagsErrorDetail(t *testing.T, diagnostics []*tfprotov6.Diagnostic) string {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			return diagnostic.Detail
		}
	}
	t.Fatal("failed operation must produce an error diagnostic")
	return ""
}

func TestConfigTags_missingCatalogReadScope(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPLookup {
			configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
			return
		}
		t.Errorf("name resolution failure must stop before config writes: %s %s", request.Method, request.URL.Path)
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	configuration := fixture.config(t, configTagsNames("Payments"))

	diagnostics := fixture.rejectedChange(t, configTagsState{}, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "tags:read") || !strings.Contains(detail, "Payments") || !strings.Contains(detail, configTagsHTTPProjectID) {
		t.Fatalf("catalog permission diagnostic must identify scope, name, and project: %s", detail)
	}
}

func TestConfigTags_taggedCreationExplainsConfigScopes(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPLookup {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPTag)
			return
		}
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	configuration := fixture.config(t, configTagsNames("Payments"))

	diagnostics := fixture.rejectedChange(t, configTagsState{}, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "configs:create") || !strings.Contains(detail, "config-settings:update") {
		t.Fatalf("tagged creation diagnostic must identify both required config scopes: %s", detail)
	}
}

func TestConfigTags_untaggedCreationExplainsOnlyCreateScope(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != configTagsHTTPEndpoint {
			t.Errorf("untagged creation must need no catalog access: %s %s", request.Method, request.URL.Path)
		}
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	configuration := fixture.config(t, tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil))

	diagnostics := fixture.rejectedChange(t, configTagsState{}, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "configs:create") || strings.Contains(detail, "config-settings:update") || strings.Contains(detail, "tags:read") {
		t.Fatalf("untagged creation must require only configs:create: %s", detail)
	}
}

func TestConfigTags_updateExplainsSettingsScope(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPAssignedConfig)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPLookup {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPTag)
			return
		}
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	imported := fixture.refresh(t, configTagsState{value: fixture.config(t, configTagsNames("Payments"))})
	configuration := fixture.withDescription(t, fixture.config(t, configTagsNames("Payments")), "Edited description")

	diagnostics := fixture.rejectedChange(t, imported, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "config-settings:update") || strings.Contains(detail, "configs:create") {
		t.Fatalf("assignment update diagnostic must identify config-settings:update: %s", detail)
	}
}

func configTagsHTTPConfigReadOnly(w http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
		configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPAssignedConfig)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPEndpoint {
		configTagsHTTPJSON(w, http.StatusOK, "["+configTagsHTTPAssignedConfig+"]")
		return
	}
	configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
}

func (fixture configTagsTest) httpDataSourceState(t *testing.T, name string) tftypes.Value {
	t.Helper()

	definition, err := fixture.server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, definition.Diagnostics)
	typeOf := definition.DataSourceSchemas[name].ValueType()
	attributes := typeOf.(tftypes.Object).AttributeTypes
	values := make(map[string]tftypes.Value, len(attributes))
	for name, typeOf := range attributes {
		values[name] = tftypes.NewValue(typeOf, nil)
	}
	values["project_id"] = tftypes.NewValue(tftypes.String, configTagsHTTPProjectID)
	if _, hasKey := attributes["key"]; hasKey {
		values["key"] = tftypes.NewValue(tftypes.String, "checkout-flag")
	}
	read, err := fixture.server.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{
		TypeName: name, Config: configTagsDynamic(t, tftypes.NewValue(typeOf, values)),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, read.Diagnostics)
	state, err := read.State.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestConfigTags_resourceReadWithoutCatalogAccess(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, configTagsHTTPConfigReadOnly)
	prior := configTagsState{value: fixture.config(t, configTagsNames("payments"))}

	refreshed := fixture.refresh(t, prior)

	if got := fixture.names(t, refreshed); !slices.Equal(got, []string{"Payments"}) {
		t.Fatalf("config-only read must expose assigned names: %v", got)
	}
}

func TestConfigTags_singleDataSourceWithoutCatalogAccess(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, configTagsHTTPConfigReadOnly)

	state := fixture.httpDataSourceState(t, "configdirector_config")

	var attributes map[string]tftypes.Value
	if err := state.As(&attributes); err != nil {
		t.Fatal(err)
	}
	if !attributes["tags"].Equal(configTagsNames("Payments")) {
		t.Fatalf("config-only data-source read must expose assigned names: %s", attributes["tags"])
	}
}

func TestConfigTags_listDataSourceWithoutCatalogAccess(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, configTagsHTTPConfigReadOnly)

	state := fixture.httpDataSourceState(t, "configdirector_configs")

	var attributes map[string]tftypes.Value
	if err := state.As(&attributes); err != nil {
		t.Fatal(err)
	}
	var configs []tftypes.Value
	if err := attributes["configs"].As(&configs); err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected one assigned config, got %d", len(configs))
	}
	if err := configs[0].As(&attributes); err != nil {
		t.Fatal(err)
	}
	if !attributes["tags"].Equal(configTagsNames("Payments")) {
		t.Fatalf("config-only list read must expose assigned names: %s", attributes["tags"])
	}
}

func TestConfigTags_omittedUpdateWithoutCatalogAccess(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPatch && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			var settings map[string]json.RawMessage
			if err := json.NewDecoder(request.Body).Decode(&settings); err != nil {
				t.Errorf("decoding config settings: %v", err)
				configTagsHTTPJSON(w, http.StatusBadRequest, `{"error":"invalid settings"}`)
				return
			}
			if _, exists := settings["tagIds"]; exists {
				t.Error("omitted tags must omit tagIds on the wire")
				configTagsHTTPJSON(w, http.StatusBadRequest, `{"error":"tags were supplied"}`)
				return
			}
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPAssignedConfig)
			return
		}
		configTagsHTTPConfigReadOnly(w, request)
	})
	prior := fixture.refresh(t, configTagsState{value: fixture.config(t, configTagsNames("Payments"))})
	omitted := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil)
	configuration := fixture.withDescription(t, fixture.config(t, omitted), "Unrelated edit")

	updated := fixture.change(t, prior, configuration)

	if got := fixture.names(t, updated); !slices.Equal(got, []string{"Payments"}) {
		t.Fatalf("omitted update without catalog access must retain assigned names: %v", got)
	}
}

func TestConfigTags_emptyUpdateWithoutCatalogAccess(t *testing.T) {
	var cleared atomic.Bool
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPatch && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			var settings struct {
				TagIDs *[]string `json:"tagIds"`
			}
			if err := json.NewDecoder(request.Body).Decode(&settings); err != nil {
				t.Errorf("decoding assignment settings: %v", err)
				configTagsHTTPJSON(w, http.StatusBadRequest, `{"error":"invalid settings"}`)
				return
			}
			if settings.TagIDs == nil || len(*settings.TagIDs) != 0 {
				t.Error("empty tags must send an explicit empty tagIds array")
				configTagsHTTPJSON(w, http.StatusBadRequest, `{"error":"expected empty assignments"}`)
				return
			}
			cleared.Store(true)
			configTagsHTTPJSON(w, http.StatusOK, `{}`)
			return
		}
		if cleared.Load() && request.Method == http.MethodGet && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			configTagsHTTPJSON(w, http.StatusOK, `{"id":"12345678-9abc-4def-8012-3456789abcde","projectId":"01234567-89ab-4cde-8f01-23456789abcd","key":"checkout-flag","role":"flag","lifetime":"temporary","type":"boolean","state":"new","client":true,"server":true,"deprecatedKeys":[],"tags":[]}`)
			return
		}
		configTagsHTTPConfigReadOnly(w, request)
	})
	prior := fixture.refresh(t, configTagsState{value: fixture.config(t, configTagsNames("Payments"))})

	updated := fixture.change(t, prior, fixture.config(t, configTagsNames()))

	if got := fixture.names(t, updated); len(got) != 0 {
		t.Fatalf("empty update without catalog access must clear assigned names: %v", got)
	}
}

func TestConfigTags_emptyCreationExplainsOnlyCreateScope(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != configTagsHTTPEndpoint {
			t.Errorf("empty creation must need no catalog access: %s %s", request.Method, request.URL.Path)
		}
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	configuration := fixture.config(t, configTagsNames())

	diagnostics := fixture.rejectedChange(t, configTagsState{}, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "configs:create") || strings.Contains(detail, "config-settings:update") || strings.Contains(detail, "tags:read") {
		t.Fatalf("empty creation must require only configs:create: %s", detail)
	}
}

func TestConfigTags_validationFailurePreservesAPIError(t *testing.T) {
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPLookup {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPTag)
			return
		}
		configTagsHTTPJSON(w, http.StatusBadRequest, `{"error":"invalid config settings"}`)
	})
	configuration := fixture.config(t, configTagsNames("Payments"))

	diagnostics := fixture.rejectedChange(t, configTagsState{}, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "status 400") || !strings.Contains(detail, "invalid config settings") || strings.Contains(detail, "configs:create") || strings.Contains(detail, "config-settings:update") {
		t.Fatalf("validation failure must retain the API error without permission guidance: %s", detail)
	}
}

func TestConfigTags_updateReadbackFailureExplainsReadScope(t *testing.T) {
	var updated atomic.Bool
	fixture := newConfigTagsHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == configTagsHTTPLookup {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPTag)
			return
		}
		if request.Method == http.MethodPatch && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			updated.Store(true)
			configTagsHTTPJSON(w, http.StatusOK, `{}`)
			return
		}
		if !updated.Load() && request.Method == http.MethodGet && request.URL.Path == configTagsHTTPEndpoint+"/checkout-flag" {
			configTagsHTTPJSON(w, http.StatusOK, configTagsHTTPAssignedConfig)
			return
		}
		configTagsHTTPJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	})
	prior := fixture.refresh(t, configTagsState{value: fixture.config(t, configTagsNames("Payments"))})
	configuration := fixture.withDescription(t, fixture.config(t, configTagsNames("Payments")), "Edited description")

	diagnostics := fixture.rejectedChange(t, prior, configuration)

	detail := configTagsErrorDetail(t, diagnostics)
	if !strings.Contains(detail, "configs:read") || !strings.Contains(detail, "status 403") {
		t.Fatalf("update readback failure must identify configs:read: %s", detail)
	}
}
