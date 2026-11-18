package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/config"
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

var integrationCredentialFields = []string{"apiKey", "secretKey", "serviceAccountUsername", "serviceAccountSecret", "serviceAccountToken", "personalApiKey", "webhookUrl"}

var amplitudeSettings = map[string]string{"data_center": "us", "api_key": "amplitude-api-key", "secret_key": "amplitude-secret-key"}
var mixpanelSettings = map[string]string{"data_residency": "eu", "project_id": "3212345", "service_account_username": "mixpanel-service-account", "service_account_secret": "mixpanel-service-secret"}
var datadogSettings = map[string]string{"site": "datadoghq.eu", "api_key": "datadog-api-key"}
var grafanaSettings = map[string]string{"url": "https://grafana.example.com", "service_account_token": "grafana-service-account-token"}
var posthogSettings = map[string]string{"host": "eu", "project_id": "42", "personal_api_key": "posthog-personal-api-key"}

const incidentsWebhookURL = "https://hooks.slack.com/services/T0ACME0001/B0INCIDENTS/incidentsWebhookSecret"
const outagesWebhookURL = "https://hooks.slack.com/services/T0ACME0001/B0OUTAGES/outagesWebhookSecret"

var sortedSlackStartingEventTypes = []string{"config.archived", "config.created", "config.restored", "config.updated", "targeting_rules.updated"}
var sortedSlackStartingAlertKinds = []string{"client-requested-server-only-config", "config-not-found", "config-type-mismatch", "default-value-mismatch", "default-value-served-too-often", "server-requested-client-only-config"}

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
	api.saved = map[string]any{
		"id": integrationStubID, "name": strings.TrimSpace(name), "kind": kind,
		"enabled": body["enabled"], "disabledReason": nil,
		"projectIds": body["projectIds"], "environmentIds": body["environmentIds"], "tagIds": body["tagIds"],
		"createdAt": createdAt, "updatedAt": fmt.Sprintf("2026-11-08T10:%02d:00.000Z", api.saves),
	}
	if kind == "slack" {
		api.saved["settings"] = savedSlackSettings(settings)
		maps.Copy(api.saved, savedSlackSelection(body))
	} else {
		api.saved["settings"] = savedAnalyticsSettings(settings)
	}
	api.saves++
	respondWithJSON(response, http.StatusOK, api.saved)
}

func settingsWithoutCredentials(settings map[string]any) map[string]any {
	saved := maps.Clone(settings)
	for _, field := range integrationCredentialFields {
		delete(saved, field)
	}
	return saved
}

func savedSlackSettings(settings map[string]any) map[string]any {
	saved := settingsWithoutCredentials(settings)
	messagePrefix, _ := saved["messagePrefix"].(string)
	saved["messagePrefix"] = strings.TrimSpace(messagePrefix)
	if _, sent := saved["postResolvedAlerts"]; !sent {
		saved["postResolvedAlerts"] = true
	}
	return saved
}

func savedAnalyticsSettings(settings map[string]any) map[string]any {
	saved := settingsWithoutCredentials(settings)
	if _, overridden := saved["endpointUrl"]; !overridden {
		saved["endpointUrl"] = nil
	}
	for _, field := range []string{"url", "host"} {
		if text, present := saved[field].(string); present {
			saved[field] = strings.TrimRight(strings.TrimSpace(text), "/")
		}
	}
	return saved
}

func savedSlackSelection(body map[string]any) map[string]any {
	selection := map[string]any{
		"eventTypes":      []any{"config.created", "config.updated", "config.archived", "config.restored", "targeting_rules.updated"},
		"alertKinds":      []any{"config-not-found", "client-requested-server-only-config", "server-requested-client-only-config", "config-type-mismatch", "default-value-mismatch", "default-value-served-too-often"},
		"minimumPriority": "high",
	}
	for field := range selection {
		if sent, present := body[field]; present {
			selection[field] = sent
		}
	}
	return selection
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

func (protocol integrationProtocol) slackConfig(t *testing.T, settings map[string]tftypes.Value, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	declaredSettings := map[string]tftypes.Value{"webhook_url": tftypes.NewValue(tftypes.String, incidentsWebhookURL)}
	maps.Copy(declaredSettings, settings)
	configured := map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "#incidents"),
		"kind":     tftypes.NewValue(tftypes.String, "slack"),
		"settings": protocol.settingsValue(declaredSettings),
	}
	maps.Copy(configured, attributes)
	return protocol.config(t, configured)
}

func incidentsSettings() map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"message_prefix":       tftypes.NewValue(tftypes.String, "@here"),
		"post_resolved_alerts": tftypes.NewValue(tftypes.Bool, false),
	}
}

func incidentsSelection() map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"event_types":      stringSetOf(),
		"alert_kinds":      stringSetOf("default-value-served-too-often", "experiment-guardrail-worse"),
		"minimum_priority": tftypes.NewValue(tftypes.String, "critical"),
	}
}

func (protocol integrationProtocol) incidentsConfig(t *testing.T) *tfprotov6.DynamicValue {
	t.Helper()

	return protocol.slackConfig(t, incidentsSettings(), incidentsSelection())
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

	requireAttributeError(t, diagnostics, tftypes.NewAttributePath().WithAttributeName("settings").WithAttributeName(setting), sentence)
}

func requireAttributeError(t *testing.T, diagnostics []*tfprotov6.Diagnostic, expectedPath *tftypes.AttributePath, sentence string) {
	t.Helper()

	errorCount := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != tfprotov6.DiagnosticSeverityError {
			continue
		}
		errorCount++
		if diagnostic.Attribute == nil || !diagnostic.Attribute.Equal(expectedPath) {
			t.Fatalf("expected the error on %s, got it on %v", expectedPath, diagnostic.Attribute)
		}
		if diagnostic.Detail != sentence {
			t.Fatalf("expected the sentence %q, got %q", sentence, diagnostic.Detail)
		}
	}
	if errorCount != 1 {
		t.Fatalf("expected one error, got %d", errorCount)
	}
}

func sortedTextsSent(t *testing.T, sent any) []string {
	t.Helper()

	elements, isList := sent.([]any)
	if !isList {
		t.Fatalf("expected a list of texts, got %#v", sent)
	}
	texts := make([]string, len(elements))
	for i, element := range elements {
		text, isText := element.(string)
		if !isText {
			t.Fatalf("expected a list of texts, got %#v", sent)
		}
		texts[i] = text
	}
	slices.Sort(texts)
	return texts
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

func TestIntegrationResource_createSendsTheSlackWebhookUrlAndWhatTheIntegrationPosts(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))

	requireSameBody(t, api.lastRequestBody(t, http.MethodPost), map[string]any{
		"name": "#incidents", "kind": "slack",
		"settings": map[string]any{"webhookUrl": incidentsWebhookURL, "messagePrefix": "@here", "postResolvedAlerts": false},
		"enabled":  true, "projectIds": []any{}, "environmentIds": []any{}, "tagIds": []any{},
		"eventTypes": []any{}, "alertKinds": []any{"default-value-served-too-often", "experiment-guardrail-worse"}, "minimumPriority": "critical",
	})
}

func TestIntegrationResource_createOfASlackIntegrationLeftAtItsStartingSelectionSendsThatSelection(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.slackConfig(t, nil, nil))

	sent := api.lastRequestBody(t, http.MethodPost)
	requireSameBody(t, api.lastSettingsSent(t, http.MethodPost), map[string]any{"webhookUrl": incidentsWebhookURL, "postResolvedAlerts": true})
	if got := sortedTextsSent(t, sent["eventTypes"]); !slices.Equal(got, sortedSlackStartingEventTypes) {
		t.Fatalf("expected the starting event types %v, got %v", sortedSlackStartingEventTypes, got)
	}
	if got := sortedTextsSent(t, sent["alertKinds"]); !slices.Equal(got, sortedSlackStartingAlertKinds) {
		t.Fatalf("expected the starting alert kinds %v, got %v", sortedSlackStartingAlertKinds, got)
	}
	if got := sent["minimumPriority"]; got != "high" {
		t.Fatalf("expected the starting minimum priority high, got %#v", got)
	}
}

func TestIntegrationResource_planOfASlackIntegrationLeftAtItsStartingSelectionShowsThatSelection(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.slackConfig(t, nil, nil))

	if got := sortedStringsOf(t, planned["event_types"]); !slices.Equal(got, sortedSlackStartingEventTypes) {
		t.Fatalf("expected the plan to show the starting event types %v, got %v", sortedSlackStartingEventTypes, got)
	}
	if got := sortedStringsOf(t, planned["alert_kinds"]); !slices.Equal(got, sortedSlackStartingAlertKinds) {
		t.Fatalf("expected the plan to show the starting alert kinds %v, got %v", sortedSlackStartingAlertKinds, got)
	}
	if got := textOf(t, planned["minimum_priority"]); got != "high" {
		t.Fatalf("expected the plan to show the starting minimum priority high, got %s", got)
	}
	if !booleanOf(t, settingOf(t, planned["settings"], "post_resolved_alerts")) {
		t.Fatal("expected the plan to show resolved alerts posted")
	}
	if got := settingOf(t, planned["settings"], "message_prefix"); !got.IsNull() {
		t.Fatalf("expected the plan to show no message prefix, got %s", got)
	}
}

func TestIntegrationResource_planOfAnAnalyticsIntegrationHasNoSlackAttributes(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.amplitudeConfig(t, nil))

	if !planned["event_types"].IsNull() {
		t.Fatalf("expected an Amplitude integration to have no event types, got %s", planned["event_types"])
	}
	if !planned["alert_kinds"].IsNull() {
		t.Fatalf("expected an Amplitude integration to have no alert kinds, got %s", planned["alert_kinds"])
	}
	if !planned["minimum_priority"].IsNull() {
		t.Fatalf("expected an Amplitude integration to have no minimum priority, got %s", planned["minimum_priority"])
	}
	if got := settingOf(t, planned["settings"], "post_resolved_alerts"); !got.IsNull() {
		t.Fatalf("expected an Amplitude integration to have no post_resolved_alerts, got %s", got)
	}
}

func TestIntegrationResource_planLeavesTheSelectionOfAKindKnownOnlyAtApplyToTheApply(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "#incidents"),
		"kind":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"settings": protocol.settingsValue(knownSettings(map[string]string{"webhook_url": incidentsWebhookURL})),
	}))

	if planned["minimum_priority"].IsKnown() {
		t.Fatalf("expected the minimum priority to stay unknown until the kind is known, got %s", planned["minimum_priority"])
	}
	if planned["event_types"].IsKnown() {
		t.Fatalf("expected the event types to stay unknown until the kind is known, got %s", planned["event_types"])
	}
}

func TestIntegrationResource_createRecordsTheSlackIntegrationTheAPISaved(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	created := protocol.values(t, protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t)))

	if got := textOf(t, settingOf(t, created["settings"], "webhook_url")); got != incidentsWebhookURL {
		t.Fatalf("expected the state to keep the configured webhook URL, got %s", got)
	}
	if got := textOf(t, settingOf(t, created["settings"], "message_prefix")); got != "@here" {
		t.Fatalf("expected the message prefix the API saved, got %s", got)
	}
	if booleanOf(t, settingOf(t, created["settings"], "post_resolved_alerts")) {
		t.Fatal("expected resolved alerts left unposted, as the API saved")
	}
	if created["event_types"].IsNull() || len(sortedStringsOf(t, created["event_types"])) != 0 {
		t.Fatalf("expected the empty event types the API saved, got %s", created["event_types"])
	}
	if got := sortedStringsOf(t, created["alert_kinds"]); !slices.Equal(got, []string{"default-value-served-too-often", "experiment-guardrail-worse"}) {
		t.Fatalf("expected the alert kinds the API saved, got %v", got)
	}
	if got := textOf(t, created["minimum_priority"]); got != "critical" {
		t.Fatalf("expected the minimum priority the API saved, got %s", got)
	}
}

func TestIntegrationResource_refreshReadsTheSlackIntegrationAndKeepsTheConfiguredWebhookUrl(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := textOf(t, settingOf(t, refreshed["settings"], "webhook_url")); got != incidentsWebhookURL {
		t.Fatalf("expected the refresh to keep the webhook URL, got %s", got)
	}
	if booleanOf(t, settingOf(t, refreshed["settings"], "post_resolved_alerts")) {
		t.Fatal("expected the refresh to read resolved alerts left unposted")
	}
}

func TestIntegrationResource_updateOfASlackIntegrationSendsWhatItPostsAndLeavesTheWebhookUrlOut(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))
	settings := incidentsSettings()
	settings["message_prefix"] = tftypes.NewValue(tftypes.String, "@channel")

	protocol.apply(t, created, protocol.slackConfig(t, settings, incidentsSelection()))

	requireSameBody(t, api.lastRequestBody(t, http.MethodPut), map[string]any{
		"name": "#incidents", "kind": "slack",
		"settings": map[string]any{"messagePrefix": "@channel", "postResolvedAlerts": false},
		"enabled":  true, "projectIds": []any{}, "environmentIds": []any{}, "tagIds": []any{},
		"eventTypes": []any{}, "alertKinds": []any{"default-value-served-too-often", "experiment-guardrail-worse"}, "minimumPriority": "critical",
	})
}

func TestIntegrationResource_updateWithAnotherWebhookUrlSendsIt(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))
	settings := incidentsSettings()
	settings["webhook_url"] = tftypes.NewValue(tftypes.String, outagesWebhookURL)

	protocol.apply(t, created, protocol.slackConfig(t, settings, incidentsSelection()))

	if got := api.lastSettingsSent(t, http.MethodPut)["webhookUrl"]; got != outagesWebhookURL {
		t.Fatalf("expected the update to send the new webhook URL, got %#v", got)
	}
}

func TestIntegrationResource_messagePrefixRemovedFromTheConfigurationLeavesMessagesWithoutOne(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))
	settings := incidentsSettings()
	delete(settings, "message_prefix")

	updated := protocol.values(t, protocol.apply(t, created, protocol.slackConfig(t, settings, incidentsSelection())))

	if _, sent := api.lastSettingsSent(t, http.MethodPut)["messagePrefix"]; sent {
		t.Fatal("expected the update to leave the message prefix out, which saves none")
	}
	if got := settingOf(t, updated["settings"], "message_prefix"); !got.IsNull() {
		t.Fatalf("expected the state to have no message prefix, got %s", got)
	}
}

func TestIntegrationResource_selectionRemovedFromTheConfigurationReturnsToTheStartingSelection(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))

	protocol.apply(t, created, protocol.slackConfig(t, nil, nil))

	sent := api.lastRequestBody(t, http.MethodPut)
	if got := sortedTextsSent(t, sent["eventTypes"]); !slices.Equal(got, sortedSlackStartingEventTypes) {
		t.Fatalf("expected the starting event types %v, got %v", sortedSlackStartingEventTypes, got)
	}
	if got := sortedTextsSent(t, sent["alertKinds"]); !slices.Equal(got, sortedSlackStartingAlertKinds) {
		t.Fatalf("expected the starting alert kinds %v, got %v", sortedSlackStartingAlertKinds, got)
	}
	if got := sent["minimumPriority"]; got != "high" {
		t.Fatalf("expected the starting minimum priority high, got %#v", got)
	}
	if got := api.lastSettingsSent(t, http.MethodPut)["postResolvedAlerts"]; got != true {
		t.Fatalf("expected resolved alerts posted again, got %#v", got)
	}
}

func TestIntegrationResource_planOfAMinimumPriorityRemovedFromTheConfigurationLeavesTheUpdateTimeToTheAPI(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))
	selection := incidentsSelection()
	delete(selection, "minimum_priority")

	planned := protocol.plannedValues(t, created, protocol.slackConfig(t, incidentsSettings(), selection))

	if got := textOf(t, planned["minimum_priority"]); got != "high" {
		t.Fatalf("expected the plan to return the minimum priority to high, got %s", got)
	}
	if planned["updated_at"].IsKnown() {
		t.Fatalf("the update time must stay unknown until the API saves the change, got %s", planned["updated_at"])
	}
}

func TestIntegrationResource_planOfPostResolvedAlertsRemovedFromTheConfigurationLeavesTheUpdateTimeToTheAPI(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))
	settings := incidentsSettings()
	delete(settings, "post_resolved_alerts")

	planned := protocol.plannedValues(t, created, protocol.slackConfig(t, settings, incidentsSelection()))

	if !booleanOf(t, settingOf(t, planned["settings"], "post_resolved_alerts")) {
		t.Fatal("expected the plan to post resolved alerts again")
	}
	if planned["updated_at"].IsKnown() {
		t.Fatalf("the update time must stay unknown until the API saves the change, got %s", planned["updated_at"])
	}
}

func TestIntegrationResource_planRestoresTheStartingMinimumPriorityChangedOutsideTerraform(t *testing.T) {
	protocol, api := newIntegrationStubProtocol(t)
	configuration := protocol.slackConfig(t, nil, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["minimumPriority"] = "low"
	})
	refreshed := protocol.refresh(t, created)

	planned := protocol.plannedValues(t, refreshed, configuration)

	if got := textOf(t, protocol.values(t, refreshed)["minimum_priority"]); got != "low" {
		t.Fatalf("expected the refresh to show the minimum priority saved outside Terraform, got %s", got)
	}
	if got := textOf(t, planned["minimum_priority"]); got != "high" {
		t.Fatalf("expected the plan to restore the starting minimum priority, got %s", got)
	}
}

func TestIntegrationResource_planAfterARefreshOfASlackIntegrationLeftAtItsStartingSelectionChangesNothing(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.slackConfig(t, nil, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)
	refreshed := protocol.refresh(t, created)

	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_planAfterARefreshOfASlackIntegrationWithEveryAttributeDeclaredChangesNothing(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.incidentsConfig(t)
	created := protocol.apply(t, protocol.absent(t), configuration)
	refreshed := protocol.refresh(t, created)

	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_keepsTheDeclaredMessagePrefixTheAPITrimmed(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	configuration := protocol.slackConfig(t, map[string]tftypes.Value{"message_prefix": tftypes.NewValue(tftypes.String, " @here ")}, nil)
	created := protocol.apply(t, protocol.absent(t), configuration)

	refreshed := protocol.refresh(t, created)

	if got := textOf(t, settingOf(t, protocol.values(t, created)["settings"], "message_prefix")); got != " @here " {
		t.Fatalf("expected the create to keep the declared message prefix, got %q", got)
	}
	protocol.requireNoPlannedChange(t, refreshed, configuration)
}

func TestIntegrationResource_importOfASlackIntegrationReadsEverythingButTheWebhookUrl(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	protocol.apply(t, protocol.absent(t), protocol.incidentsConfig(t))

	imported := protocol.values(t, protocol.importByID(t, integrationStubID))

	if got := textOf(t, imported["kind"]); got != "slack" {
		t.Fatalf("expected the saved kind, got %q", got)
	}
	if got := textOf(t, settingOf(t, imported["settings"], "message_prefix")); got != "@here" {
		t.Fatalf("expected the saved message prefix, got %q", got)
	}
	if booleanOf(t, settingOf(t, imported["settings"], "post_resolved_alerts")) {
		t.Fatal("expected resolved alerts left unposted, as saved")
	}
	if got := sortedStringsOf(t, imported["alert_kinds"]); !slices.Equal(got, []string{"default-value-served-too-often", "experiment-guardrail-worse"}) {
		t.Fatalf("expected the saved alert kinds, got %v", got)
	}
	if got := textOf(t, imported["minimum_priority"]); got != "critical" {
		t.Fatalf("expected the saved minimum priority, got %q", got)
	}
	if got := settingOf(t, imported["settings"], "webhook_url"); !got.IsNull() {
		t.Fatalf("expected the import to hold no webhook URL, got %s", got)
	}
}

func TestIntegrationResource_acceptsASlackIntegrationWithOnlyItsWebhookUrl(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.slackConfig(t, nil, nil))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_acceptsASlackIntegrationWithEveryAttributeDeclared(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.incidentsConfig(t))

	requireNoErrorDiagnostics(t, diagnostics)
}

func TestIntegrationResource_refusesASlackIntegrationWithoutAWebhookUrl(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.slackConfig(t, map[string]tftypes.Value{"webhook_url": tftypes.NewValue(tftypes.String, nil)}, nil))

	requireSettingError(t, diagnostics, "webhook_url", "The Slack webhook URL must have the form https://hooks.slack.com/services/…")
}

func TestIntegrationResource_refusesAnAnalyticsSettingOnASlackIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.slackConfig(t, map[string]tftypes.Value{"api_key": tftypes.NewValue(tftypes.String, "amplitude-api-key")}, nil))

	requireSettingError(t, diagnostics, "api_key", "api_key does not apply to the slack kind. Remove it from settings.")
}

func TestIntegrationResource_refusesAnEndpointUrlOnASlackIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.slackConfig(t, map[string]tftypes.Value{"endpoint_url": tftypes.NewValue(tftypes.String, "https://annotations.example.com/slack")}, nil))

	requireSettingError(t, diagnostics, "endpoint_url", "endpoint_url does not apply to the slack kind. Remove it from settings.")
}

func TestIntegrationResource_refusesAMessagePrefixOnAnAnalyticsIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.kindConfig(t, "amplitude", withSetting(amplitudeSettings, "message_prefix", "@here"), nil))

	requireSettingError(t, diagnostics, "message_prefix", "message_prefix does not apply to the amplitude kind. Remove it from settings.")
}

func TestIntegrationResource_refusesPostResolvedAlertsOnAnAnalyticsIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)
	settings := knownSettings(amplitudeSettings)
	settings["post_resolved_alerts"] = tftypes.NewValue(tftypes.Bool, true)

	diagnostics := protocol.validate(t, protocol.config(t, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "Product analytics"),
		"kind":     tftypes.NewValue(tftypes.String, "amplitude"),
		"settings": protocol.settingsValue(settings),
	}))

	requireSettingError(t, diagnostics, "post_resolved_alerts", "post_resolved_alerts does not apply to the amplitude kind. Remove it from settings.")
}

func TestIntegrationResource_refusesEventTypesOnAnAnalyticsIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.amplitudeConfig(t, map[string]tftypes.Value{"event_types": stringSetOf("config.updated")}))

	requireAttributeError(t, diagnostics, tftypes.NewAttributePath().WithAttributeName("event_types"), "event_types does not apply to the amplitude kind. Remove it.")
}

func TestIntegrationResource_refusesAlertKindsOnAnAnalyticsIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.amplitudeConfig(t, map[string]tftypes.Value{"alert_kinds": stringSetOf()}))

	requireAttributeError(t, diagnostics, tftypes.NewAttributePath().WithAttributeName("alert_kinds"), "alert_kinds does not apply to the amplitude kind. Remove it.")
}

func TestIntegrationResource_refusesAMinimumPriorityOnAnAnalyticsIntegration(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.amplitudeConfig(t, map[string]tftypes.Value{"minimum_priority": tftypes.NewValue(tftypes.String, "high")}))

	requireAttributeError(t, diagnostics, tftypes.NewAttributePath().WithAttributeName("minimum_priority"), "minimum_priority does not apply to the amplitude kind. Remove it.")
}

func TestIntegrationResource_refusesAMinimumPriorityThatIsNotAPriority(t *testing.T) {
	protocol, _ := newIntegrationStubProtocol(t)

	diagnostics := protocol.validate(t, protocol.slackConfig(t, nil, map[string]tftypes.Value{"minimum_priority": tftypes.NewValue(tftypes.String, "urgent")}))

	if got := firstErrorAttribute(t, diagnostics); got != fmt.Sprint(tftypes.AttributeName("minimum_priority")) {
		t.Fatalf("expected the error on minimum_priority, got it on %s", got)
	}
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
	expected := []string{"api_key", "personal_api_key", "secret_key", "service_account_secret", "service_account_token", "service_account_username", "webhook_url"}
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

func TestIntegrationResource_acceptsAnEndpointUrlOnAnAnalyticsKind(t *testing.T) {
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
	Name               string         `json:"name"`
	Kind               string         `json:"kind"`
	URL                string         `json:"url"`
	Settings           map[string]any `json:"settings"`
	Enabled            bool           `json:"enabled"`
	EventTypes         []string       `json:"eventTypes"`
	AlertKinds         []string       `json:"alertKinds"`
	MinimumPriority    string         `json:"minimumPriority"`
	CredentialsSavedAt string         `json:"credentialsSavedAt"`
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

func (acceptance integrationAcceptance) namedSlackConfig(t *testing.T, settings, selection map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	attributes := map[string]tftypes.Value{
		"name":    tftypes.NewValue(tftypes.String, acceptance.name),
		"enabled": tftypes.NewValue(tftypes.Bool, false),
	}
	maps.Copy(attributes, selection)
	return acceptance.slackConfig(t, settings, attributes)
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

func requireSavedSetting(t *testing.T, saved integrationSavedByTheAPI, field string, expected any) {
	t.Helper()

	value, present := saved.Settings[field]
	if !present || value != expected {
		t.Fatalf("expected the API to have %s %#v, it has %+v", field, expected, saved.Settings)
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

func TestAccIntegrationResource_createsASlackIntegrationThatNeverShowsItsWebhookUrl(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)

	created := acceptance.create(t, acceptance.namedSlackConfig(t, incidentsSettings(), incidentsSelection()))

	saved := acceptance.saved(t, created)
	if saved.Name != acceptance.name || saved.Kind != "slack" || saved.Enabled {
		t.Fatalf("expected a disabled Slack integration with the declared name, the API has %+v", saved)
	}
	if saved.URL != "https://hooks.slack.com/services/" {
		t.Fatalf("expected the API to show only the start every Slack webhook URL shares, it has %s", saved.URL)
	}
	requireSavedSetting(t, saved, "messagePrefix", "@here")
	requireSavedSetting(t, saved, "postResolvedAlerts", false)
	if len(saved.Settings) != 2 {
		t.Fatalf("expected the API to return only the message prefix and the resolved alerts switch, it has %+v", saved.Settings)
	}
	if len(saved.EventTypes) != 0 {
		t.Fatalf("expected no event types, the API has %v", saved.EventTypes)
	}
	if got := slices.Sorted(slices.Values(saved.AlertKinds)); !slices.Equal(got, []string{"default-value-served-too-often", "experiment-guardrail-worse"}) {
		t.Fatalf("expected the declared alert kinds, the API has %v", got)
	}
	if saved.MinimumPriority != "critical" {
		t.Fatalf("expected the minimum priority critical, the API has %s", saved.MinimumPriority)
	}
}

func TestAccIntegrationResource_slackPrefixAndMinimumPriorityChangeKeepsTheWebhookUrlAndTheAlertKinds(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedSlackConfig(t, incidentsSettings(), incidentsSelection()))
	savedBefore := acceptance.saved(t, created)
	settings := incidentsSettings()
	settings["message_prefix"] = tftypes.NewValue(tftypes.String, "@channel")
	selection := incidentsSelection()
	selection["minimum_priority"] = tftypes.NewValue(tftypes.String, "high")

	updated := acceptance.apply(t, created, acceptance.namedSlackConfig(t, settings, selection))

	saved := acceptance.saved(t, updated)
	requireSavedSetting(t, saved, "messagePrefix", "@channel")
	if saved.MinimumPriority != "high" {
		t.Fatalf("expected the minimum priority high, the API has %s", saved.MinimumPriority)
	}
	if got := slices.Sorted(slices.Values(saved.AlertKinds)); !slices.Equal(got, []string{"default-value-served-too-often", "experiment-guardrail-worse"}) {
		t.Fatalf("expected the alert kinds to stay as declared, the API has %v", got)
	}
	if saved.CredentialsSavedAt != savedBefore.CredentialsSavedAt {
		t.Fatalf("expected the webhook URL to stay as saved at %s, the API has %s", savedBefore.CredentialsSavedAt, saved.CredentialsSavedAt)
	}
}

func TestAccIntegrationResource_anotherWebhookUrlReplacesTheSavedOne(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedSlackConfig(t, incidentsSettings(), incidentsSelection()))
	savedBefore := acceptance.saved(t, created)
	settings := incidentsSettings()
	settings["webhook_url"] = tftypes.NewValue(tftypes.String, outagesWebhookURL)

	updated := acceptance.apply(t, created, acceptance.namedSlackConfig(t, settings, incidentsSelection()))

	saved := acceptance.saved(t, updated)
	if saved.CredentialsSavedAt == savedBefore.CredentialsSavedAt {
		t.Fatalf("expected the webhook URL to be saved again, the API still has %s", saved.CredentialsSavedAt)
	}
	requireSavedSetting(t, saved, "messagePrefix", "@here")
}

func TestAccIntegrationResource_slackImportReadsEverythingButTheWebhookUrl(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedSlackConfig(t, incidentsSettings(), incidentsSelection()))

	imported := acceptance.values(t, acceptance.importByID(t, textOf(t, acceptance.values(t, created)["id"])))

	if got := textOf(t, imported["kind"]); got != "slack" {
		t.Fatalf("expected the imported kind, got %q", got)
	}
	if got := textOf(t, settingOf(t, imported["settings"], "message_prefix")); got != "@here" {
		t.Fatalf("expected the imported message prefix, got %q", got)
	}
	if got := textOf(t, imported["minimum_priority"]); got != "critical" {
		t.Fatalf("expected the imported minimum priority, got %q", got)
	}
	if got := settingOf(t, imported["settings"], "webhook_url"); !got.IsNull() {
		t.Fatalf("expected the import to hold no webhook URL, got %s", got)
	}
}

func TestAccIntegrationResource_destroysASlackIntegration(t *testing.T) {
	acceptance := newIntegrationAcceptance(t)
	created := acceptance.create(t, acceptance.namedSlackConfig(t, nil, nil))

	requireNoErrorDiagnostics(t, acceptance.destroy(t, created))

	if got := acceptance.requestSaved(t, created).StatusCode; got != http.StatusNotFound {
		t.Fatalf("expected the API to no longer have the integration, got HTTP %d", got)
	}
}

func testAccSlackIntegrationConfig(name, webhookURL string) string {
	return fmt.Sprintf(`
resource "configdirector_integration" "test" {
  name    = %q
  kind    = "slack"
  enabled = false
  settings = {
    webhook_url = %q
  }
}
`, name, webhookURL)
}

func testAccIncidentsIntegrationConfig(name, webhookURL, messagePrefix, minimumPriority string) string {
	return fmt.Sprintf(`
resource "configdirector_integration" "test" {
  name    = %q
  kind    = "slack"
  enabled = false
  settings = {
    webhook_url          = %q
    message_prefix       = %q
    post_resolved_alerts = false
  }
  event_types      = []
  alert_kinds      = ["default-value-served-too-often", "experiment-guardrail-worse"]
  minimum_priority = %q
}
`, name, webhookURL, messagePrefix, minimumPriority)
}

const testAccIncidentsIntegrationConfigFromASensitiveVariable = `
variable "webhook_url" {
  type      = string
  sensitive = true
}

variable "name" {
  type = string
}

resource "configdirector_integration" "test" {
  name    = var.name
  kind    = "slack"
  enabled = false
  settings = {
    webhook_url    = var.webhook_url
    message_prefix = "@here"
  }
  event_types      = []
  minimum_priority = "critical"
}
`

type expectTheWebhookUrlHidden struct{}

func (check expectTheWebhookUrlHidden) CheckPlan(ctx context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, change := range req.Plan.ResourceChanges {
		if change.Address != "configdirector_integration.test" {
			continue
		}
		afterSensitive, _ := change.Change.AfterSensitive.(map[string]any)
		settings, _ := afterSensitive["settings"].(map[string]any)
		if settings["webhook_url"] != true {
			resp.Error = fmt.Errorf("expected the plan to hide settings.webhook_url, its hidden values are %v", change.Change.AfterSensitive)
		}
		return
	}
	resp.Error = errors.New("expected a plan for configdirector_integration.test")
}

func TestAccIntegrationResource_terraformAppliesEverySlackStepWithoutDrift(t *testing.T) {
	name := testAccIntegrationNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix)
	startingSelection := []statecheck.StateCheck{
		statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("event_types"), knownvalue.SetExact([]knownvalue.Check{
			knownvalue.StringExact("config.created"), knownvalue.StringExact("config.updated"), knownvalue.StringExact("config.archived"),
			knownvalue.StringExact("config.restored"), knownvalue.StringExact("targeting_rules.updated"),
		})),
		statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("alert_kinds"), knownvalue.SetExact([]knownvalue.Check{
			knownvalue.StringExact("config-not-found"), knownvalue.StringExact("client-requested-server-only-config"),
			knownvalue.StringExact("server-requested-client-only-config"), knownvalue.StringExact("config-type-mismatch"),
			knownvalue.StringExact("default-value-mismatch"), knownvalue.StringExact("default-value-served-too-often"),
		})),
		statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("minimum_priority"), knownvalue.StringExact("high")),
		statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("post_resolved_alerts"), knownvalue.Bool(true)),
		statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("message_prefix"), knownvalue.Null()),
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            testAccSlackIntegrationConfig(name, incidentsWebhookURL),
				ConfigStateChecks: startingSelection,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectTheWebhookUrlHidden{}},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccIncidentsIntegrationConfig(name, incidentsWebhookURL, "@here", "critical"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate), expectTheWebhookUrlHidden{}},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccIncidentsIntegrationConfig(name, incidentsWebhookURL, "@channel", "high"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("settings").AtMapKey("message_prefix"), knownvalue.StringExact("@channel")),
					statecheck.ExpectKnownValue("configdirector_integration.test", tfjsonpath.New("minimum_priority"), knownvalue.StringExact("high")),
				},
			},
			{
				Config: testAccIncidentsIntegrationConfig(name, outagesWebhookURL, "@channel", "high"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate), expectTheWebhookUrlHidden{}},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config:            testAccSlackIntegrationConfig(name, outagesWebhookURL),
				ConfigStateChecks: startingSelection,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_integration.test", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            "configdirector_integration.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"settings.webhook_url"},
			},
		},
	})
}

func TestAccIntegrationResource_slackWebhookUrlNeverReachesTheLogs(t *testing.T) {
	name := testAccIntegrationNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix)
	logPath := filepath.Join(t.TempDir(), "terraform.log")
	t.Setenv("TF_LOG", "TRACE")
	t.Setenv("TF_ACC_LOG", "TRACE")
	t.Setenv("TF_ACC_LOG_PATH", logPath)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:          testAccIncidentsIntegrationConfigFromASensitiveVariable,
				ConfigVariables: config.Variables{"name": config.StringVariable(name), "webhook_url": config.StringVariable(incidentsWebhookURL)},
			},
			{
				Config:          testAccIncidentsIntegrationConfigFromASensitiveVariable,
				ConfigVariables: config.Variables{"name": config.StringVariable(name), "webhook_url": config.StringVariable(outagesWebhookURL)},
			},
			{
				Config:                  testAccIncidentsIntegrationConfigFromASensitiveVariable,
				ConfigVariables:         config.Variables{"name": config.StringVariable(name), "webhook_url": config.StringVariable(outagesWebhookURL)},
				ResourceName:            "configdirector_integration.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"settings.webhook_url"},
			},
		},
	})

	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logs, []byte("configdirector_integration")) {
		t.Fatal("expected the log to record the integration's applies")
	}
	for _, line := range bytes.Split(logs, []byte("\n")) {
		if bytes.Contains(line, []byte("incidentsWebhookSecret")) || bytes.Contains(line, []byte("outagesWebhookSecret")) {
			t.Fatalf("expected the log to never hold the webhook URL, this line holds it:\n%s", line)
		}
	}
}

func TestAccIntegrationResource_analyticsIntegrationAppliedWith0100PlansNoChange(t *testing.T) {
	name := testAccIntegrationNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix)
	t.Setenv(resource.EnvTfAccProviderNamespace, "configdirector")
	configurationWrittenFor0100 := testAccAmplitudeIntegrationConfig(name, "us", "amplitude-secret-key")

	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		Steps: []resource.TestStep{
			{
				ExternalProviders: map[string]resource.ExternalProvider{
					"configdirector": {Source: "ConfigDirector/configdirector", VersionConstraint: "0.10.0"},
				},
				Config: configurationWrittenFor0100,
			},
			{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   configurationWrittenFor0100,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
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
