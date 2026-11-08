package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const integrationStubID = "7b3e9f1c-2a4d-4c6e-9f8b-1d2c3b4a5e6f"
const integrationStubPath = "/v1/integrations/" + integrationStubID
const testAccIntegrationNamePrefix = "tf-acc-"

var integrationCredentialFields = []string{"apiKey", "secretKey", "serviceAccountUsername", "serviceAccountSecret", "serviceAccountToken", "personalApiKey"}

var amplitudeSettings = map[string]string{"data_center": "us", "api_key": "amplitude-api-key", "secret_key": "amplitude-secret-key"}
var mixpanelSettings = map[string]string{"data_residency": "eu", "project_id": "3212345", "service_account_username": "mixpanel-service-account", "service_account_secret": "mixpanel-service-secret"}
var datadogSettings = map[string]string{"site": "datadoghq.eu", "api_key": "datadog-api-key"}
var grafanaSettings = map[string]string{"url": "https://grafana.example.com", "service_account_token": "grafana-service-account-token"}
var posthogSettings = map[string]string{"host": "eu", "project_id": "42", "personal_api_key": "posthog-personal-api-key"}

type integrationStubRequest struct {
	method string
	path   string
	body   map[string]any
}

type integrationStubAPI struct {
	mutex    sync.Mutex
	server   *httptest.Server
	saved    map[string]any
	saves    int
	requests []integrationStubRequest
}

func newIntegrationStubAPI(t *testing.T) *integrationStubAPI {
	t.Helper()

	api := &integrationStubAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(api.server.Close)
	return api
}

func (api *integrationStubAPI) serve(response http.ResponseWriter, request *http.Request) {
	api.mutex.Lock()
	defer api.mutex.Unlock()

	var body map[string]any
	_ = json.NewDecoder(request.Body).Decode(&body)
	api.requests = append(api.requests, integrationStubRequest{method: request.Method, path: request.URL.Path, body: body})

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/integrations":
		api.respondSaving(response, body, "2026-11-08T10:00:00.000Z")
	case request.URL.Path != integrationStubPath || api.saved == nil:
		respondWithJSON(response, http.StatusNotFound, map[string]any{"error": "Not found"})
	case request.Method == http.MethodGet:
		respondWithJSON(response, http.StatusOK, api.saved)
	case request.Method == http.MethodPut:
		api.respondSaving(response, body, api.saved["createdAt"].(string))
	case request.Method == http.MethodDelete:
		api.saved = nil
		response.WriteHeader(http.StatusNoContent)
	}
}

func (api *integrationStubAPI) respondSaving(response http.ResponseWriter, body map[string]any, createdAt string) {
	settings, settingsAreObject := body["settings"].(map[string]any)
	name, nameIsString := body["name"].(string)
	kind, kindIsString := body["kind"].(string)
	if !settingsAreObject || !nameIsString || !kindIsString {
		respondWithJSON(response, http.StatusBadRequest, map[string]any{"error": "Invalid body"})
		return
	}
	for _, field := range []string{"enabled", "projectIds", "environmentIds", "tagIds"} {
		if body[field] == nil {
			respondWithJSON(response, http.StatusBadRequest, map[string]any{"error": "Invalid body: " + field})
			return
		}
	}
	saved := maps.Clone(settings)
	for _, field := range integrationCredentialFields {
		delete(saved, field)
	}
	if _, overridden := saved["endpointUrl"]; !overridden {
		saved["endpointUrl"] = nil
	}
	for _, field := range []string{"url", "host"} {
		if text, present := saved[field].(string); present {
			saved[field] = strings.TrimRight(strings.TrimSpace(text), "/")
		}
	}
	api.saved = map[string]any{
		"id": integrationStubID, "name": strings.TrimSpace(name), "kind": kind, "settings": saved,
		"enabled": body["enabled"], "disabledReason": nil,
		"projectIds": body["projectIds"], "environmentIds": body["environmentIds"], "tagIds": body["tagIds"],
		"createdAt": createdAt, "updatedAt": fmt.Sprintf("2026-11-08T10:%02d:00.000Z", api.saves),
	}
	api.saves++
	respondWithJSON(response, http.StatusOK, api.saved)
}

func (api *integrationStubAPI) lastRequestBody(t *testing.T, method string) map[string]any {
	t.Helper()

	api.mutex.Lock()
	defer api.mutex.Unlock()
	for i := len(api.requests) - 1; i >= 0; i-- {
		if api.requests[i].method == method {
			return api.requests[i].body
		}
	}
	t.Fatalf("expected a %s request to the Admin API", method)
	return nil
}

func (api *integrationStubAPI) lastSettingsSent(t *testing.T, method string) map[string]any {
	t.Helper()

	settings, isObject := api.lastRequestBody(t, method)["settings"].(map[string]any)
	if !isObject {
		t.Fatalf("expected the %s request to carry settings", method)
	}
	return settings
}

func (api *integrationStubAPI) changeOutsideTerraform(change func(saved map[string]any)) {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	change(api.saved)
}

func (api *integrationStubAPI) deleteOutsideTerraform() {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	api.saved = nil
}

func (api *integrationStubAPI) holdsAnIntegration() bool {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	return api.saved != nil
}

type integrationProtocol struct {
	resourceProtocol
}

func newIntegrationStubProtocol(t *testing.T) (integrationProtocol, *integrationStubAPI) {
	t.Helper()

	api := newIntegrationStubAPI(t)
	return integrationProtocol{newResourceProtocol(t, "configdirector_integration", api.server.URL, "integration-test-token")}, api
}

func (protocol integrationProtocol) settingsValue(settings map[string]tftypes.Value) tftypes.Value {
	typeOf := protocol.typeOf.AttributeTypes["settings"].(tftypes.Object)
	values := make(map[string]tftypes.Value, len(typeOf.AttributeTypes))
	for name, attributeType := range typeOf.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	maps.Copy(values, settings)
	return tftypes.NewValue(typeOf, values)
}

func knownSettings(settings map[string]string) map[string]tftypes.Value {
	values := make(map[string]tftypes.Value, len(settings))
	for name, value := range settings {
		values[name] = tftypes.NewValue(tftypes.String, value)
	}
	return values
}

func (protocol integrationProtocol) kindConfig(t *testing.T, kind string, settings map[string]string, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	configured := map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Product analytics"),
		"kind":     tftypes.NewValue(tftypes.String, kind),
		"settings": protocol.settingsValue(knownSettings(settings)),
	}
	maps.Copy(configured, attributes)
	return protocol.config(t, configured)
}

func (protocol integrationProtocol) amplitudeConfig(t *testing.T, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	return protocol.kindConfig(t, "amplitude", amplitudeSettings, attributes)
}

func (protocol integrationProtocol) validate(t *testing.T, configuration *tfprotov6.DynamicValue) []*tfprotov6.Diagnostic {
	t.Helper()

	validated, err := protocol.server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName: protocol.typeName, Config: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	return validated.Diagnostics
}

func (protocol integrationProtocol) requireNoPlannedChange(t *testing.T, state, configuration *tfprotov6.DynamicValue) {
	t.Helper()

	planned := protocol.plan(t, state, configuration)
	if len(planned.RequiresReplace) != 0 {
		t.Fatalf("expected no replacement, got one for %v", planned.RequiresReplace)
	}
	plannedValues := protocol.values(t, planned.PlannedState)
	for name, value := range protocol.values(t, state) {
		if !value.Equal(plannedValues[name]) {
			t.Fatalf("expected the plan to leave %s as it is, got %s planned over %s", name, plannedValues[name], value)
		}
	}
}

func withSetting(settings map[string]string, name, value string) map[string]string {
	changed := maps.Clone(settings)
	changed[name] = value
	return changed
}

func withoutSetting(settings map[string]string, name string) map[string]string {
	changed := maps.Clone(settings)
	delete(changed, name)
	return changed
}

func settingOf(t *testing.T, settings tftypes.Value, name string) tftypes.Value {
	t.Helper()

	var values map[string]tftypes.Value
	if err := settings.As(&values); err != nil {
		t.Fatal(err)
	}
	value, present := values[name]
	if !present {
		t.Fatalf("settings must have a %s attribute", name)
	}
	return value
}

func requireSettingError(t *testing.T, diagnostics []*tfprotov6.Diagnostic, setting, sentence string) {
	t.Helper()

	expectedPath := tftypes.NewAttributePath().WithAttributeName("settings").WithAttributeName(setting)
	errorCount := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != tfprotov6.DiagnosticSeverityError {
			continue
		}
		errorCount++
		if diagnostic.Attribute == nil || !diagnostic.Attribute.Equal(expectedPath) {
			t.Fatalf("expected the error on settings.%s, got it on %v", setting, diagnostic.Attribute)
		}
		if diagnostic.Detail != sentence {
			t.Fatalf("expected the sentence %q, got %q", sentence, diagnostic.Detail)
		}
	}
	if errorCount != 1 {
		t.Fatalf("expected one error, got %d", errorCount)
	}
}

func requireSameBody(t *testing.T, sent, expected map[string]any) {
	t.Helper()

	if !reflect.DeepEqual(sent, expected) {
		t.Fatalf("expected the request body %#v, got %#v", expected, sent)
	}
}

func TestIntegrationResource_createSendsTheAmplitudeSettingsAndCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	requireSameBody(t, api.lastRequestBody(t, http.MethodPost), map[string]any{
		"name": "Product analytics", "kind": "amplitude",
		"settings": map[string]any{"dataCenter": "us", "apiKey": "amplitude-api-key", "secretKey": "amplitude-secret-key"},
		"enabled":  true, "projectIds": []any{}, "environmentIds": []any{}, "tagIds": []any{},
	})
}

func TestIntegrationResource_createRecordsTheIntegrationTheAPISaved(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	created := protocol.values(t, protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil)))

	if got := textOf(t, created["id"]); got != integrationStubID {
		t.Fatalf("expected the integration ID %s, got %s", integrationStubID, got)
	}
	if got := textOf(t, created["created_at"]); got != "2026-11-08T10:00:00.000Z" {
		t.Fatalf("expected the creation time the API recorded, got %s", got)
	}
	if got := textOf(t, settingOf(t, created["settings"], "data_center")); got != "us" {
		t.Fatalf("expected the data center the API saved, got %s", got)
	}
	if got := textOf(t, settingOf(t, created["settings"], "secret_key")); got != "amplitude-secret-key" {
		t.Fatalf("expected the state to keep the configured secret key, got %s", got)
	}
	if got := sortedStringsOf(t, created["project_ids"]); !slices.Equal(got, []string{}) {
		t.Fatalf("expected an empty project filter, got %v", got)
	}
}

func TestIntegrationResource_marksEverySecretSettingSensitive(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	settings := protocol.attribute(t, "settings").NestedType.Attributes

	sensitiveSettings := []string{}
	for _, setting := range settings {
		if setting.Sensitive {
			sensitiveSettings = append(sensitiveSettings, setting.Name)
		}
	}
	slices.Sort(sensitiveSettings)
	expected := []string{"api_key", "personal_api_key", "secret_key", "service_account_secret", "service_account_token", "service_account_username"}
	if !slices.Equal(sensitiveSettings, expected) {
		t.Fatalf("expected the sensitive settings %v, got %v", expected, sensitiveSettings)
	}
}

func TestIntegrationResource_createSendsTheMixpanelSettingsAndCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "mixpanel", mixpanelSettings, nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPost), map[string]any{
		"dataResidency": "eu", "projectId": "3212345",
		"serviceAccountUsername": "mixpanel-service-account", "serviceAccountSecret": "mixpanel-service-secret",
	})
}

func TestIntegrationResource_createSendsTheDatadogSettingsAndCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "datadog", datadogSettings, nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPost), map[string]any{
		"site": "datadoghq.eu", "apiKey": "datadog-api-key",
	})
}

func TestIntegrationResource_createSendsTheGrafanaSettingsAndCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "grafana", grafanaSettings, nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPost), map[string]any{
		"url": "https://grafana.example.com", "serviceAccountToken": "grafana-service-account-token",
	})
}

func TestIntegrationResource_createSendsThePostHogSettingsAndCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "posthog", posthogSettings, nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPost), map[string]any{
		"host": "eu", "projectId": "42", "personalApiKey": "posthog-personal-api-key",
	})
}

func TestIntegrationResource_createSendsTheEndpointUrlOverride(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	settings := withSetting(amplitudeSettings, "endpoint_url", "https://annotations.example.com/amplitude")

	created := protocol.values(t, protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "amplitude", settings, nil)))

	if got := api.lastSettingsSent(t, http.MethodPost)["endpointUrl"]; got != "https://annotations.example.com/amplitude" {
		t.Fatalf("expected the create to send the endpoint URL, got %#v", got)
	}
	if got := textOf(t, settingOf(t, created["settings"], "endpoint_url")); got != "https://annotations.example.com/amplitude" {
		t.Fatalf("expected the state to hold the endpoint URL the API saved, got %s", got)
	}
}

func TestIntegrationResource_updateWithoutTheEndpointUrlRemovesTheOverride(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	settings := withSetting(amplitudeSettings, "endpoint_url", "https://annotations.example.com/amplitude")
	created := protocol.apply(t, protocol.absent(t), protocol.kindConfig(t, "amplitude", settings, nil))

	updated := protocol.values(t, protocol.apply(t, created, protocol.amplitudeConfig(t, nil)))

	if _, sent := api.lastSettingsSent(t, http.MethodPut)["endpointUrl"]; sent {
		t.Fatal("expected the update to leave the endpoint URL out")
	}
	if got := settingOf(t, updated["settings"], "endpoint_url"); !got.IsNull() {
		t.Fatalf("expected the state to have no endpoint URL, got %s", got)
	}
}

func TestIntegrationResource_settingChangeUpdatesTheIntegrationInPlace(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	planned := protocol.plan(t, created, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "data_center", "eu"), nil))

	if len(planned.RequiresReplace) != 0 {
		t.Fatalf("expected a setting change to keep the integration, got a replacement for %v", planned.RequiresReplace)
	}
}

func TestIntegrationResource_kindChangeReplacesTheIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	planned := protocol.plan(t, created, protocol.kindConfig(t, "datadog", datadogSettings, nil))

	if len(planned.RequiresReplace) != 1 || !planned.RequiresReplace[0].Equal(tftypes.NewAttributePath().WithAttributeName("kind")) {
		t.Fatalf("expected a kind change to replace the integration, got %v", planned.RequiresReplace)
	}
}

func TestIntegrationResource_updateWithUnchangedCredentialsLeavesThemOut(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	protocol.apply(t, created, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "data_center", "eu"), nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPut), map[string]any{"dataCenter": "eu"})
}

func TestIntegrationResource_updateOfOneCredentialSendsTheWholePair(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	protocol.apply(t, created, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "secret_key", "rotated-secret-key"), nil))

	requireSameBody(t, api.lastSettingsSent(t, http.MethodPut), map[string]any{
		"dataCenter": "us", "apiKey": "amplitude-api-key", "secretKey": "rotated-secret-key",
	})
}

func TestIntegrationResource_updateRecordsTheNewFilters(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	updated := protocol.values(t, protocol.apply(t, created, protocol.amplitudeConfig(t, map[string]tftypes.Value{
		"project_ids":     stringSetOf("6a1f2e3d-4c5b-4a69-8877-665544332211"),
		"environment_ids": stringSetOf("11111111-4c5b-4a69-8877-665544332211"),
		"tag_ids":         stringSetOf("22222222-4c5b-4a69-8877-665544332211"),
	})))

	if got := sortedStringsOf(t, updated["project_ids"]); !slices.Equal(got, []string{"6a1f2e3d-4c5b-4a69-8877-665544332211"}) {
		t.Fatalf("expected the project filter the API saved, got %v", got)
	}
	if got := sortedStringsOf(t, updated["environment_ids"]); !slices.Equal(got, []string{"11111111-4c5b-4a69-8877-665544332211"}) {
		t.Fatalf("expected the environment filter the API saved, got %v", got)
	}
	if got := sortedStringsOf(t, updated["tag_ids"]); !slices.Equal(got, []string{"22222222-4c5b-4a69-8877-665544332211"}) {
		t.Fatalf("expected the tag filter the API saved, got %v", got)
	}
}

func TestIntegrationResource_refreshKeepsTheConfiguredCredentials(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := textOf(t, settingOf(t, refreshed["settings"], "api_key")); got != "amplitude-api-key" {
		t.Fatalf("expected the refresh to keep the API key, got %s", got)
	}
	if got := textOf(t, settingOf(t, refreshed["settings"], "secret_key")); got != "amplitude-secret-key" {
		t.Fatalf("expected the refresh to keep the secret key, got %s", got)
	}
}

func TestIntegrationResource_planAfterARefreshChangesNothing(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)
	refreshed := protocol.refresh(t, created)

	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_planRestoresASettingChangedOutsideTerraform(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["settings"].(map[string]any)["dataCenter"] = "eu"
	})
	refreshed := protocol.refresh(t, created)

	planned := protocol.plannedValues(t, refreshed, configuration)

	if got := textOf(t, settingOf(t, protocol.values(t, refreshed)["settings"], "data_center")); got != "eu" {
		t.Fatalf("expected the refresh to show the data center saved outside Terraform, got %s", got)
	}
	if got := textOf(t, settingOf(t, planned["settings"], "data_center")); got != "us" {
		t.Fatalf("expected the plan to restore the configured data center, got %s", got)
	}
}

func TestIntegrationResource_planEnablesAnIntegrationDisabledAfterFailedDeliveries(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["enabled"] = false
		saved["disabledReason"] = "failures"
	})
	refreshed := protocol.refresh(t, created)

	planned := protocol.plannedValues(t, refreshed, configuration)

	if !booleanOf(t, planned["enabled"]) {
		t.Fatal("expected the plan to enable the integration again")
	}
}

func TestIntegrationResource_refreshForgetsAnIntegrationDeletedOutsideTerraform(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))
	api.deleteOutsideTerraform()

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if refreshed != nil {
		t.Fatalf("a deleted integration must leave the state so it can be created again, got %v", refreshed)
	}
}

func TestIntegrationResource_keepsTheDeclaredGrafanaUrlTheAPIShortened(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.kindConfig(t, "grafana", withSetting(grafanaSettings, "url", "https://grafana.example.com/"), nil)
	created := protocol.apply(t, protocol.absent(t), configuration)

	refreshed := protocol.refresh(t, created)

	if got := textOf(t, settingOf(t, protocol.values(t, created)["settings"], "url")); got != "https://grafana.example.com/" {
		t.Fatalf("expected the create to keep the declared URL, got %q", got)
	}
	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_keepsTheDeclaredPostHogHostTheAPIShortened(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.kindConfig(t, "posthog", withSetting(posthogSettings, "host", "https://posthog.example.com/"), nil)
	created := protocol.apply(t, protocol.absent(t), configuration)

	refreshed := protocol.refresh(t, created)

	if got := textOf(t, settingOf(t, protocol.values(t, created)["settings"], "host")); got != "https://posthog.example.com/" {
		t.Fatalf("expected the create to keep the declared host, got %q", got)
	}
	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_keepsTheDeclaredNameTheAPITrimmed(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, " Product analytics ")})
	created := protocol.apply(t, protocol.absent(t), configuration)

	refreshed := protocol.refresh(t, created)

	if got := textOf(t, protocol.values(t, created)["name"]); got != " Product analytics " {
		t.Fatalf("expected the create to keep the declared name, got %q", got)
	}
	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_importReadsEverythingButTheCredentials(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, map[string]tftypes.Value{
		"enabled":     tftypes.NewValue(tftypes.Bool, false),
		"project_ids": stringSetOf("6a1f2e3d-4c5b-4a69-8877-665544332211"),
	}))

	imported := protocol.values(t, protocol.importByID(t, integrationStubID))

	if got := textOf(t, imported["name"]); got != "Product analytics" {
		t.Fatalf("expected the saved name, got %q", got)
	}
	if got := textOf(t, imported["kind"]); got != "amplitude" {
		t.Fatalf("expected the saved kind, got %q", got)
	}
	if booleanOf(t, imported["enabled"]) {
		t.Fatal("expected the saved enabled state, got enabled")
	}
	if got := sortedStringsOf(t, imported["project_ids"]); !slices.Equal(got, []string{"6a1f2e3d-4c5b-4a69-8877-665544332211"}) {
		t.Fatalf("expected the saved project filter, got %v", got)
	}
	if got := textOf(t, settingOf(t, imported["settings"], "data_center")); got != "us" {
		t.Fatalf("expected the saved data center, got %q", got)
	}
	if got := settingOf(t, imported["settings"], "api_key"); !got.IsNull() {
		t.Fatalf("expected the import to hold no API key, got %s", got)
	}
	if got := settingOf(t, imported["settings"], "secret_key"); !got.IsNull() {
		t.Fatalf("expected the import to hold no secret key, got %s", got)
	}
}

func TestIntegrationResource_destroyDeletesTheIntegration(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	diagnostics := protocol.destroy(t, created)

	requireNoErrorDiagnostics(t, diagnostics)
	if api.holdsAnIntegration() {
		t.Fatal("expected the destroy to delete the integration")
	}
}

func TestIntegrationResource_destroySucceedsWhenTheIntegrationIsAlreadyGone(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))
	api.deleteOutsideTerraform()

	diagnostics := protocol.destroy(t, created)

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsCompleteAmplitudeSettings(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.amplitudeConfig(t, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsCompleteMixpanelSettings(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "mixpanel", mixpanelSettings, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsCompleteDatadogSettings(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "datadog", datadogSettings, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsCompleteGrafanaSettings(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "grafana", grafanaSettings, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsCompletePostHogSettings(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "posthog", posthogSettings, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsAnEndpointUrlOnAnyKind(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	settings := withSetting(datadogSettings, "endpoint_url", "https://annotations.example.com/datadog")

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "datadog", settings, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_refusesAnUnknownKind(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "segment", amplitudeSettings, nil))

	if got := firstErrorAttribute(t, diagnostics); got != fmt.Sprint(tftypes.AttributeName("kind")) {
		t.Fatalf("expected the error on kind, got it on %s", got)
	}
}

func TestIntegrationResource_refusesASettingOfAnotherKind(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	settings := withSetting(amplitudeSettings, "site", "datadoghq.eu")

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", settings, nil))

	requireSettingError(t, diagnostics, "site", "site does not apply to the amplitude kind. Remove it from settings.")
}

func TestIntegrationResource_leavesASettingKnownOnlyAtApplyToTheApply(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.config(t, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Product analytics"),
		"kind": tftypes.NewValue(tftypes.String, "amplitude"),
		"settings": protocol.settingsValue(map[string]tftypes.Value{
			"data_center": tftypes.NewValue(tftypes.String, "us"),
			"api_key":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"secret_key":  tftypes.NewValue(tftypes.String, "amplitude-secret-key"),
		}),
	}))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_leavesSettingsKnownOnlyAtApplyToTheApply(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.config(t, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Product analytics"),
		"kind":     tftypes.NewValue(tftypes.String, "amplitude"),
		"settings": tftypes.NewValue(protocol.typeOf.AttributeTypes["settings"], tftypes.UnknownValue),
	}))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_leavesAKindKnownOnlyAtApplyToTheApply(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.config(t, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Product analytics"),
		"kind":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"settings": protocol.settingsValue(nil),
	}))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_refusesAnAmplitudeIntegrationWithoutADataCenter(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", withoutSetting(amplitudeSettings, "data_center"), nil))

	requireSettingError(t, diagnostics, "data_center", "The Amplitude data center must be us or eu")
}

func TestIntegrationResource_refusesAnAmplitudeIntegrationWithoutAnAPIKey(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", withoutSetting(amplitudeSettings, "api_key"), nil))

	requireSettingError(t, diagnostics, "api_key", "The API key is required")
}

func TestIntegrationResource_refusesAnAmplitudeIntegrationWithoutASecretKey(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", withoutSetting(amplitudeSettings, "secret_key"), nil))

	requireSettingError(t, diagnostics, "secret_key", "The secret key is required")
}

func TestIntegrationResource_refusesAMixpanelIntegrationWithoutADataResidency(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "mixpanel", withoutSetting(mixpanelSettings, "data_residency"), nil))

	requireSettingError(t, diagnostics, "data_residency", "The Mixpanel data residency must be us, eu or in")
}

func TestIntegrationResource_refusesAMixpanelIntegrationWithoutAProjectId(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "mixpanel", withoutSetting(mixpanelSettings, "project_id"), nil))

	requireSettingError(t, diagnostics, "project_id", "The project id is required")
}

func TestIntegrationResource_refusesAMixpanelIntegrationWithoutAServiceAccountUsername(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "mixpanel", withoutSetting(mixpanelSettings, "service_account_username"), nil))

	requireSettingError(t, diagnostics, "service_account_username", "The service account username is required")
}

func TestIntegrationResource_refusesAMixpanelIntegrationWithoutAServiceAccountSecret(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "mixpanel", withoutSetting(mixpanelSettings, "service_account_secret"), nil))

	requireSettingError(t, diagnostics, "service_account_secret", "The service account secret is required")
}

func TestIntegrationResource_refusesADatadogIntegrationWithoutASite(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "datadog", withoutSetting(datadogSettings, "site"), nil))

	requireSettingError(t, diagnostics, "site", "The Datadog site must be one of: datadoghq.com, us3.datadoghq.com, us5.datadoghq.com, datadoghq.eu, ap1.datadoghq.com")
}

func TestIntegrationResource_refusesADatadogIntegrationWithoutAnAPIKey(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "datadog", withoutSetting(datadogSettings, "api_key"), nil))

	requireSettingError(t, diagnostics, "api_key", "The API key is required")
}

func TestIntegrationResource_refusesAGrafanaIntegrationWithoutAUrl(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "grafana", withoutSetting(grafanaSettings, "url"), nil))

	requireSettingError(t, diagnostics, "url", "The Grafana URL is required")
}

func TestIntegrationResource_refusesAGrafanaIntegrationWithoutAServiceAccountToken(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "grafana", withoutSetting(grafanaSettings, "service_account_token"), nil))

	requireSettingError(t, diagnostics, "service_account_token", "The service account token is required")
}

func TestIntegrationResource_refusesAPostHogIntegrationWithoutAHost(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "posthog", withoutSetting(posthogSettings, "host"), nil))

	requireSettingError(t, diagnostics, "host", "The PostHog host must be us, eu or the https:// URL of a self-hosted instance")
}

func TestIntegrationResource_refusesAPostHogIntegrationWithoutAProjectId(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "posthog", withoutSetting(posthogSettings, "project_id"), nil))

	requireSettingError(t, diagnostics, "project_id", "The project id is required")
}

func TestIntegrationResource_refusesAPostHogIntegrationWithoutAPersonalAPIKey(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "posthog", withoutSetting(posthogSettings, "personal_api_key"), nil))

	requireSettingError(t, diagnostics, "personal_api_key", "The personal API key is required")
}

type integrationAcceptance struct {
	integrationProtocol
	api  *client.Client
	name string
}

type integrationSavedByTheAPI struct {
	Name               string             `json:"name"`
	Kind               string             `json:"kind"`
	URL                string             `json:"url"`
	Settings           map[string]*string `json:"settings"`
	Enabled            bool               `json:"enabled"`
	CredentialsSavedAt string             `json:"credentialsSavedAt"`
}

func newIntegrationAcceptance(t *testing.T) integrationAcceptance {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("real API tests require TF_ACC=1")
	}
	testAccPreCheck(t)
	token := os.Getenv("CONFIGDIRECTOR_TOKEN")
	return integrationAcceptance{
		integrationProtocol: integrationProtocol{newResourceProtocol(t, "configdirector_integration", testAccBaseURL(), token)},
		api:                 client.New(testAccBaseURL(), token),
		name:                testAccIntegrationNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix),
	}
}

func (acceptance integrationAcceptance) namedAmplitudeConfig(t *testing.T, settings map[string]string) *tfprotov6.DynamicValue {
	t.Helper()

	return acceptance.kindConfig(t, "amplitude", settings, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, acceptance.name),
	})
}

func (acceptance integrationAcceptance) create(t *testing.T, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	created := acceptance.apply(t, acceptance.absent(t), configuration)
	id := textOf(t, acceptance.values(t, created)["id"])
	t.Cleanup(func() {
		err := acceptance.api.DeleteIntegration(context.Background(), id)
		var apiError *client.APIError
		alreadyDeleted := errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
		if err != nil && !alreadyDeleted {
			t.Errorf("deleting test integration: %s", err)
		}
	})
	return created
}

func (acceptance integrationAcceptance) requestSaved(t *testing.T, state *tfprotov6.DynamicValue) *http.Response {
	t.Helper()

	id := textOf(t, acceptance.values(t, state)["id"])
	request, err := http.NewRequest(http.MethodGet, acceptance.api.BaseURL+"/v1/integrations/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+acceptance.api.Token)
	response, err := acceptance.api.HTTPClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (acceptance integrationAcceptance) saved(t *testing.T, state *tfprotov6.DynamicValue) integrationSavedByTheAPI {
	t.Helper()

	response := acceptance.requestSaved(t, state)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reading the saved integration: HTTP %d", response.StatusCode)
	}
	var saved integrationSavedByTheAPI
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func requireSavedSetting(t *testing.T, saved integrationSavedByTheAPI, field, expected string) {
	t.Helper()

	value, present := saved.Settings[field]
	if !present || value == nil || *value != expected {
		t.Fatalf("expected the API to have %s %q, it has %+v", field, expected, saved.Settings)
	}
}

func TestAccIntegrationResource_createsTheIntegration(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)

	created := acceptance.create(t, acceptance.namedAmplitudeConfig(t, amplitudeSettings))

	saved := acceptance.saved(t, created)
	if saved.Name != acceptance.name || saved.Kind != "amplitude" || !saved.Enabled {
		t.Fatalf("expected an enabled Amplitude integration with the declared name, the API has %+v", saved)
	}
	if saved.URL != "https://amplitude.com/api/3/annotations" {
		t.Fatalf("expected the URL composed for the US data center, the API has %s", saved.URL)
	}
	requireSavedSetting(t, saved, "dataCenter", "us")
	if len(saved.Settings) != 2 || saved.Settings["endpointUrl"] != nil {
		t.Fatalf("expected the API to return only the data center and an empty endpoint URL, it has %+v", saved.Settings)
	}
}

func TestAccIntegrationResource_settingChangeRecomposesTheURLAndKeepsTheCredentials(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedAmplitudeConfig(t, amplitudeSettings))
	savedBefore := acceptance.saved(t, created)

	updated := acceptance.apply(t, created, acceptance.namedAmplitudeConfig(t, withSetting(amplitudeSettings, "data_center", "eu")))

	saved := acceptance.saved(t, updated)
	requireSavedSetting(t, saved, "dataCenter", "eu")
	if saved.URL != "https://analytics.eu.amplitude.com/api/3/annotations" {
		t.Fatalf("expected the URL composed for the EU data center, the API has %s", saved.URL)
	}
	if saved.CredentialsSavedAt != savedBefore.CredentialsSavedAt {
		t.Fatalf("expected the credentials to stay as saved at %s, the API has %s", savedBefore.CredentialsSavedAt, saved.CredentialsSavedAt)
	}
}

func TestAccIntegrationResource_changedSecretKeyReplacesTheCredentials(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedAmplitudeConfig(t, amplitudeSettings))
	savedBefore := acceptance.saved(t, created)

	updated := acceptance.apply(t, created, acceptance.namedAmplitudeConfig(t, withSetting(amplitudeSettings, "secret_key", "rotated-secret-key")))

	saved := acceptance.saved(t, updated)
	if saved.CredentialsSavedAt == savedBefore.CredentialsSavedAt {
		t.Fatalf("expected the credentials to be saved again, the API still has %s", saved.CredentialsSavedAt)
	}
	requireSavedSetting(t, saved, "dataCenter", "us")
}

func TestAccIntegrationResource_planAfterARefreshChangesNothing(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	configuration := acceptance.namedAmplitudeConfig(t, amplitudeSettings)
	created := acceptance.create(t, configuration)

	refreshed := acceptance.refresh(t, created)

	acceptance.requireNoPlannedChange(t, refreshed, configuration)
}

func TestAccIntegrationResource_importReadsEverythingButTheCredentials(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedAmplitudeConfig(t, amplitudeSettings))

	imported := acceptance.values(t, acceptance.importByID(t, textOf(t, acceptance.values(t, created)["id"])))

	if got := textOf(t, imported["name"]); got != acceptance.name {
		t.Fatalf("expected the imported name %q, got %q", acceptance.name, got)
	}
	if got := textOf(t, imported["kind"]); got != "amplitude" {
		t.Fatalf("expected the imported kind, got %q", got)
	}
	if got := textOf(t, settingOf(t, imported["settings"], "data_center")); got != "us" {
		t.Fatalf("expected the imported data center, got %q", got)
	}
	if got := settingOf(t, imported["settings"], "api_key"); !got.IsNull() {
		t.Fatalf("expected the import to hold no API key, got %s", got)
	}
	if got := settingOf(t, imported["settings"], "secret_key"); !got.IsNull() {
		t.Fatalf("expected the import to hold no secret key, got %s", got)
	}
}

func TestAccIntegrationResource_destroysTheIntegration(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedAmplitudeConfig(t, amplitudeSettings))

	requireNoErrorDiagnostics(t, acceptance.destroy(t, created))

	if got := acceptance.requestSaved(t, created).StatusCode; got != http.StatusNotFound {
		t.Fatalf("expected the API to no longer have the integration, got HTTP %d", got)
	}
}

func testAccAmplitudeIntegrationConfig(name, dataCenter, secretKey string) string {
	return fmt.Sprintf(`
resource "configdirector_integration" "test" {
  name = %q
  kind = "amplitude"
  settings = {
    data_center = %q
    api_key     = "amplitude-api-key"
    secret_key  = %q
  }
}
`, name, dataCenter, secretKey)
}

func TestAccIntegrationResource_terraformAppliesEveryStepWithoutDrift(t *testing.T) {
	name := testAccIntegrationNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccAmplitudeIntegrationConfig(name, "us", "amplitude-secret-key"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("data_center"), knownvalue.StringExact("us")),
					statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("secret_key"), knownvalue.StringExact("amplitude-secret-key")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccAmplitudeIntegrationConfig(name, "eu", "amplitude-secret-key"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccAmplitudeIntegrationConfig(name, "eu", "rotated-secret-key"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("secret_key"), knownvalue.StringExact("rotated-secret-key")),
				},
			},
			{
				ResourceName:            "configdirector_integration.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"settings.api_key", "settings.secret_key"},
			},
		},
	})
}

func TestAccIntegrationResource_missingSettingFailsThePlan(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "configdirector_integration" "test" {
  name = "tf-acc-refused"
  kind = "amplitude"
  settings = {
    data_center = "us"
    secret_key  = "amplitude-secret-key"
  }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+API\s+key\s+is\s+required`),
			},
		},
	})
}

func TestIntegrationResource_refusesABlankAPIKey(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "api_key", "  "), nil))

	requireSettingError(t, diagnostics, "api_key", "The API key is required")
}

func TestIntegrationResource_updatePlanLeavesTheUpdateTimeToTheAPI(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	planned := protocol.plannedValues(t, created, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "data_center", "eu"), nil))

	if planned["updated_at"].IsKnown() {
		t.Fatalf("the update time must stay unknown until the API saves the change, got %s", planned["updated_at"])
	}
}

func TestIntegrationResource_updatePlanKeepsTheID(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	planned := protocol.plannedValues(t, created, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "data_center", "eu"), nil))

	if got := textOf(t, planned["id"]); got != integrationStubID {
		t.Fatalf("expected the update to keep the integration ID, got %s", got)
	}
}

func TestIntegrationResource_firstApplyAfterAnImportSavesTheDeclaredCredentials(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, nil)
	protocol.apply(t, protocol.absent(t), configuration)
	imported := protocol.importByID(t, integrationStubID)

	planned := protocol.plannedValues(t, imported, configuration)
	protocol.apply(t, imported, configuration)

	if got := textOf(t, settingOf(t, planned["settings"], "api_key")); got != "amplitude-api-key" {
		t.Fatalf("expected the plan after the import to change the API key to its declared value, got %s", got)
	}
	if got := textOf(t, settingOf(t, planned["settings"], "secret_key")); got != "amplitude-secret-key" {
		t.Fatalf("expected the plan after the import to change the secret key to its declared value, got %s", got)
	}
	requireSameBody(t, api.lastSettingsSent(t, http.MethodPut), map[string]any{
		"dataCenter": "us", "apiKey": "amplitude-api-key", "secretKey": "amplitude-secret-key",
	})
}

func TestIntegrationResource_planAfterTheFirstApplyFollowingAnImportChangesNothing(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.amplitudeConfig(t, nil)
	protocol.apply(t, protocol.absent(t), configuration)
	imported := protocol.importByID(t, integrationStubID)

	applied := protocol.apply(t, imported, configuration)

	protocol.requireNoPlannedChange(t, protocol.refresh(t, applied), configuration)
}
