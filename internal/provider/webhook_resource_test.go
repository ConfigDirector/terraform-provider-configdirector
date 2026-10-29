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
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const webhookStubID = "4f6d2c1a-8b3e-4d5f-9a7c-2e1b0d9c8f7a"
const webhookStubSecret = "whsec_dGVycmFmb3JtIHN0dWIgc2lnbmluZyBzZWNyZXQ="
const webhookStubPath = "/v1/webhooks/" + webhookStubID
const testAccWebhookNamePrefix = "tf-acc-"

var changeEventTypesTheAPIOffers = []string{
	"config.archived", "config.created", "config.restored", "config.updated",
	"environment.created", "environment.deleted", "environment.updated",
	"project.created", "project.deleted", "project.updated",
	"segment.created", "segment.deleted", "segment.updated",
	"targeting_rules.updated",
}

type webhookStubRequest struct {
	method string
	path   string
	body   map[string]any
}

type webhookStubAPI struct {
	mutex    sync.Mutex
	server   *httptest.Server
	saved    map[string]any
	saves    int
	requests []webhookStubRequest
}

func newWebhookStubAPI(t *testing.T) *webhookStubAPI {
	t.Helper()

	api := &webhookStubAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(api.server.Close)
	return api
}

func (api *webhookStubAPI) serve(response http.ResponseWriter, request *http.Request) {
	api.mutex.Lock()
	defer api.mutex.Unlock()

	var body map[string]any
	_ = json.NewDecoder(request.Body).Decode(&body)
	api.requests = append(api.requests, webhookStubRequest{method: request.Method, path: request.URL.Path, body: body})

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/webhooks":
		api.respondSaving(response, body, "2026-10-28T10:00:00.000Z")
	case request.URL.Path != webhookStubPath || api.saved == nil:
		webhookStubJSON(response, http.StatusNotFound, map[string]any{"error": "Not found"})
	case request.Method == http.MethodGet:
		webhookStubJSON(response, http.StatusOK, api.saved)
	case request.Method == http.MethodPut:
		api.respondSaving(response, body, api.saved["createdAt"].(string))
	case request.Method == http.MethodDelete:
		api.saved = nil
		response.WriteHeader(http.StatusNoContent)
	}
}

func (api *webhookStubAPI) respondSaving(response http.ResponseWriter, body map[string]any, createdAt string) {
	headers, headersAreList := body["headers"].([]any)
	name, nameIsString := body["name"].(string)
	url, urlIsString := body["url"].(string)
	if !headersAreList || !nameIsString || !urlIsString {
		webhookStubJSON(response, http.StatusBadRequest, map[string]any{"error": "Invalid body"})
		return
	}
	for _, field := range []string{"enabled", "projectIds", "environmentIds", "tagIds", "eventTypes", "alertKinds"} {
		if body[field] == nil {
			webhookStubJSON(response, http.StatusBadRequest, map[string]any{"error": "Invalid body: " + field})
			return
		}
	}
	headerNames := make([]any, len(headers))
	for i, header := range headers {
		headerNames[i] = map[string]any{"name": header.(map[string]any)["name"]}
	}
	api.saved = map[string]any{
		"id": webhookStubID, "name": strings.TrimSpace(name), "url": strings.TrimSpace(url), "headers": headerNames,
		"enabled": body["enabled"], "disabledReason": nil,
		"projectIds": body["projectIds"], "environmentIds": body["environmentIds"], "tagIds": body["tagIds"],
		"eventTypes": body["eventTypes"], "alertKinds": body["alertKinds"],
		"secret": webhookStubSecret, "previousSecretExpiresAt": nil,
		"createdAt": createdAt, "updatedAt": fmt.Sprintf("2026-10-28T10:%02d:00.000Z", api.saves),
	}
	api.saves++
	webhookStubJSON(response, http.StatusOK, api.saved)
}

func webhookStubJSON(response http.ResponseWriter, status int, body map[string]any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func (api *webhookStubAPI) lastRequestBody(t *testing.T, method string) map[string]any {
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

func (api *webhookStubAPI) changeOutsideTerraform(change func(saved map[string]any)) {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	change(api.saved)
}

func (api *webhookStubAPI) deleteOutsideTerraform() {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	api.saved = nil
}

func (api *webhookStubAPI) holdsAWebhook() bool {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	return api.saved != nil
}

func webhookStubHeaderEqual(sent, expected any) bool {
	sentHeader, sentIsObject := sent.(map[string]any)
	expectedHeader := expected.(map[string]any)
	return sentIsObject && len(sentHeader) == len(expectedHeader) &&
		sentHeader["name"] == expectedHeader["name"] && sentHeader["value"] == expectedHeader["value"]
}

type webhookProtocol struct {
	server tfprotov6.ProviderServer
	schema *tfprotov6.Schema
	typeOf tftypes.Object
}

func newWebhookProtocol(t *testing.T, baseURL, token string) webhookProtocol {
	t.Helper()

	server := providerserver.NewProtocol6(New("test")())()
	definition, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, definition.Diagnostics)
	webhookSchema, registered := definition.ResourceSchemas["configdirector_webhook"]
	if !registered {
		t.Fatal("configdirector_webhook must be registered as a provider resource")
	}
	configured, err := server.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		Config: webhookDynamic(t, tftypes.NewValue(definition.Provider.ValueType(), map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, token), "base_url": tftypes.NewValue(tftypes.String, baseURL),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, configured.Diagnostics)
	return webhookProtocol{server: server, schema: webhookSchema, typeOf: webhookSchema.ValueType().(tftypes.Object)}
}

func newWebhookStubProtocol(t *testing.T) (webhookProtocol, *webhookStubAPI) {
	t.Helper()

	api := newWebhookStubAPI(t)
	return newWebhookProtocol(t, api.server.URL, "webhook-test-token"), api
}

func requireWebhookDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("unexpected diagnostic: %s: %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
}

func webhookErrorAttribute(t *testing.T, diagnostics []*tfprotov6.Diagnostic) string {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError && diagnostic.Attribute != nil {
			return fmt.Sprint(diagnostic.Attribute.Steps()[0])
		}
	}
	t.Fatalf("expected an error on an attribute, got %v", diagnostics)
	return ""
}

func webhookDynamic(t *testing.T, value tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	dynamic, err := tfprotov6.NewDynamicValue(value.Type(), value)
	if err != nil {
		t.Fatal(err)
	}
	return &dynamic
}

func webhookSet(values ...string) tftypes.Value {
	elements := make([]tftypes.Value, len(values))
	for i, value := range values {
		elements[i] = tftypes.NewValue(tftypes.String, value)
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elements)
}

func webhookHeaders(headers map[string]string) tftypes.Value {
	elements := make(map[string]tftypes.Value, len(headers))
	for name, value := range headers {
		elements[name] = tftypes.NewValue(tftypes.String, value)
	}
	return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elements)
}

func (protocol webhookProtocol) attribute(t *testing.T, name string) *tfprotov6.SchemaAttribute {
	t.Helper()

	for _, attribute := range protocol.schema.Block.Attributes {
		if attribute.Name == name {
			return attribute
		}
	}
	t.Fatalf("configdirector_webhook must have a %s attribute", name)
	return nil
}

func (protocol webhookProtocol) config(t *testing.T, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	values := make(map[string]tftypes.Value, len(protocol.typeOf.AttributeTypes))
	for name, typeOf := range protocol.typeOf.AttributeTypes {
		values[name] = tftypes.NewValue(typeOf, nil)
	}
	values["name"] = tftypes.NewValue(tftypes.String, "CI pipeline")
	values["url"] = tftypes.NewValue(tftypes.String, "https://ci.example.com/hooks/configdirector")
	maps.Copy(values, attributes)
	return webhookDynamic(t, tftypes.NewValue(protocol.typeOf, values))
}

func (protocol webhookProtocol) values(t *testing.T, dynamic *tfprotov6.DynamicValue) map[string]tftypes.Value {
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

func (protocol webhookProtocol) absent(t *testing.T) *tfprotov6.DynamicValue {
	t.Helper()

	return webhookDynamic(t, tftypes.NewValue(protocol.typeOf, nil))
}

func (protocol webhookProtocol) proposedNewState(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	proposed := protocol.values(t, configuration)
	previous := protocol.values(t, prior)
	for _, attribute := range protocol.schema.Block.Attributes {
		if previous != nil && attribute.Computed && proposed[attribute.Name].IsNull() {
			proposed[attribute.Name] = previous[attribute.Name]
		}
	}
	return webhookDynamic(t, tftypes.NewValue(protocol.typeOf, proposed))
}

func (protocol webhookProtocol) plan(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	t.Helper()

	planned, err := protocol.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: "configdirector_webhook", PriorState: prior, Config: configuration,
		ProposedNewState: protocol.proposedNewState(t, prior, configuration),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, planned.Diagnostics)
	return planned
}

func (protocol webhookProtocol) plannedValues(t *testing.T, prior, configuration *tfprotov6.DynamicValue) map[string]tftypes.Value {
	t.Helper()

	return protocol.values(t, protocol.plan(t, prior, configuration).PlannedState)
}

func (protocol webhookProtocol) apply(t *testing.T, prior, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	planned := protocol.plan(t, prior, configuration)
	applied, err := protocol.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_webhook", PriorState: prior, Config: configuration,
		PlannedState: planned.PlannedState, PlannedPrivate: planned.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, applied.Diagnostics)
	return applied.NewState
}

func (protocol webhookProtocol) refresh(t *testing.T, state *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	read, err := protocol.server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: "configdirector_webhook", CurrentState: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, read.Diagnostics)
	return read.NewState
}

func (protocol webhookProtocol) importByID(t *testing.T, id string) *tfprotov6.DynamicValue {
	t.Helper()

	imported, err := protocol.server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{
		TypeName: "configdirector_webhook", ID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWebhookDiagnostics(t, imported.Diagnostics)
	if len(imported.ImportedResources) != 1 {
		t.Fatalf("expected one imported webhook, got %d", len(imported.ImportedResources))
	}
	return protocol.refresh(t, imported.ImportedResources[0].State)
}

func (protocol webhookProtocol) destroy(t *testing.T, state *tfprotov6.DynamicValue) []*tfprotov6.Diagnostic {
	t.Helper()

	applied, err := protocol.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_webhook", PriorState: state, PlannedState: protocol.absent(t), Config: protocol.absent(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return applied.Diagnostics
}

func requireKnownWebhookValue(t *testing.T, value tftypes.Value) {
	t.Helper()

	if !value.IsKnown() {
		t.Fatal("expected a known value, got one left unknown until apply")
	}
}

func webhookSortedStrings(t *testing.T, value tftypes.Value) []string {
	t.Helper()

	requireKnownWebhookValue(t, value)
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

func webhookText(t *testing.T, value tftypes.Value) string {
	t.Helper()

	requireKnownWebhookValue(t, value)
	var text string
	if err := value.As(&text); err != nil {
		t.Fatal(err)
	}
	return text
}

func webhookBoolean(t *testing.T, value tftypes.Value) bool {
	t.Helper()

	requireKnownWebhookValue(t, value)
	var boolean bool
	if err := value.As(&boolean); err != nil {
		t.Fatal(err)
	}
	return boolean
}

func webhookHeaderValues(t *testing.T, value tftypes.Value) map[string]string {
	t.Helper()

	requireKnownWebhookValue(t, value)
	if value.IsNull() {
		t.Fatal("expected headers, got none")
	}
	var elements map[string]tftypes.Value
	if err := value.As(&elements); err != nil {
		t.Fatal(err)
	}
	headers := make(map[string]string, len(elements))
	for name, element := range elements {
		headers[name] = webhookText(t, element)
	}
	return headers
}

func TestWebhookResource_hidesSecretAndHeadersFromPlans(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	secret := protocol.attribute(t, "secret")
	headers := protocol.attribute(t, "headers")

	if !secret.Sensitive || !secret.Computed || secret.Optional || secret.Required {
		t.Fatalf("secret must be a sensitive attribute only the API sets, got %+v", secret)
	}
	if !headers.Sensitive {
		t.Fatalf("headers must be sensitive, got %+v", headers)
	}
}

func TestWebhookResource_createPlanReceivesEveryChangeEventTypeAndNoAlertEvent(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if got := webhookSortedStrings(t, planned["event_types"]); !slices.Equal(got, changeEventTypesTheAPIOffers) {
		t.Fatalf("expected every change event type, got %v", got)
	}
	if got := webhookSortedStrings(t, planned["alert_kinds"]); len(got) != 0 {
		t.Fatalf("expected no alert kind, got %v", got)
	}
}

func TestWebhookResource_createPlanEnablesTheWebhook(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if !webhookBoolean(t, planned["enabled"]) {
		t.Fatal("expected the webhook to be enabled")
	}
}

func TestWebhookResource_createPlanMatchesEveryProject(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if got := webhookSortedStrings(t, planned["project_ids"]); len(got) != 0 {
		t.Fatalf("expected an empty project filter, got %v", got)
	}
}

func TestWebhookResource_createPlanMatchesEveryEnvironment(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if got := webhookSortedStrings(t, planned["environment_ids"]); len(got) != 0 {
		t.Fatalf("expected an empty environment filter, got %v", got)
	}
}

func TestWebhookResource_createPlanMatchesEveryTag(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if got := webhookSortedStrings(t, planned["tag_ids"]); len(got) != 0 {
		t.Fatalf("expected an empty tag filter, got %v", got)
	}
}

func TestWebhookResource_createPlanLeavesTheSecretToTheAPI(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	planned := protocol.plannedValues(t, protocol.absent(t), protocol.config(t, nil))

	if planned["secret"].IsKnown() {
		t.Fatalf("the secret must stay unknown until the API creates it, got %s", planned["secret"])
	}
}

func TestWebhookResource_emptyEventTypesReceiveNoChangeEvent(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"event_types": webhookSet(),
		"alert_kinds": webhookSet("config-settled", "config-type-mismatch"),
	}))

	sent := api.lastRequestBody(t, http.MethodPost)
	if eventTypes, isList := sent["eventTypes"].([]any); !isList || len(eventTypes) != 0 {
		t.Fatalf("expected the create to send an empty eventTypes list, got %#v", sent["eventTypes"])
	}
	if alertKinds, isList := sent["alertKinds"].([]any); !isList || !slices.Equal(alertKinds, []any{"config-settled", "config-type-mismatch"}) {
		t.Fatalf("expected the create to send both alert kinds, got %#v", sent["alertKinds"])
	}
}

func TestWebhookResource_createSendsEveryHeaderValue(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)

	protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	}))

	sent := api.lastRequestBody(t, http.MethodPost)
	expected := []any{map[string]any{"name": "Authorization", "value": "Bearer ci-token"}}
	if headers, isList := sent["headers"].([]any); !isList || !slices.EqualFunc(headers, expected, webhookStubHeaderEqual) {
		t.Fatalf("expected the create to send the header with its value, got %#v", sent["headers"])
	}
}

func TestWebhookResource_createRecordsTheWebhookTheAPISaved(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	created := protocol.values(t, protocol.apply(t, protocol.absent(t), protocol.config(t, nil)))

	if got := webhookText(t, created["id"]); got != webhookStubID {
		t.Fatalf("expected the webhook ID %s, got %s", webhookStubID, got)
	}
	if got := webhookText(t, created["secret"]); got != webhookStubSecret {
		t.Fatalf("expected the secret the API generated, got %s", got)
	}
	if got := webhookText(t, created["created_at"]); got != "2026-10-28T10:00:00.000Z" {
		t.Fatalf("expected the creation time the API recorded, got %s", got)
	}
}

func TestWebhookResource_updatePlanKeepsTheIDAndSecret(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	planned := protocol.plannedValues(t, created, protocol.config(t, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Deploy pipeline"),
	}))

	if got := webhookText(t, planned["id"]); got != webhookStubID {
		t.Fatalf("expected the update to keep the webhook ID, got %s", got)
	}
	if got := webhookText(t, planned["secret"]); got != webhookStubSecret {
		t.Fatalf("expected the update plan to keep the secret, got %s", got)
	}
}

func TestWebhookResource_updatePlanLeavesTheUpdateTimeToTheAPI(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	planned := protocol.plannedValues(t, created, protocol.config(t, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Deploy pipeline"),
	}))

	if planned["updated_at"].IsKnown() {
		t.Fatalf("the update time must stay unknown until the API saves the change, got %s", planned["updated_at"])
	}
}

func TestWebhookResource_urlChangeUpdatesTheWebhookInPlace(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	planned := protocol.plan(t, created, protocol.config(t, map[string]tftypes.Value{
		"url": tftypes.NewValue(tftypes.String, "https://deploy.example.com/hooks/configdirector"),
	}))

	if len(planned.RequiresReplace) != 0 {
		t.Fatalf("expected a URL change to keep the webhook, got a replacement for %v", planned.RequiresReplace)
	}
}

func TestWebhookResource_updateSendsWhatTheConfigurationLeavesOut(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	protocol.apply(t, created, protocol.config(t, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "Deploy pipeline"),
	}))

	sent := api.lastRequestBody(t, http.MethodPut)
	if sent["enabled"] != true {
		t.Fatalf("expected the update to keep the webhook enabled, got %#v", sent["enabled"])
	}
	eventTypes, isList := sent["eventTypes"].([]any)
	if !isList {
		t.Fatalf("expected the update to send eventTypes, got %#v", sent["eventTypes"])
	}
	sentEventTypes := make([]string, len(eventTypes))
	for i, eventType := range eventTypes {
		sentEventTypes[i] = fmt.Sprint(eventType)
	}
	slices.Sort(sentEventTypes)
	if !slices.Equal(sentEventTypes, changeEventTypesTheAPIOffers) {
		t.Fatalf("expected the update to send every change event type, got %v", sentEventTypes)
	}
}

func TestWebhookResource_updateSendsEveryHeaderValue(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	headers := webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"})
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{"headers": headers}))

	protocol.apply(t, created, protocol.config(t, map[string]tftypes.Value{
		"headers": headers,
		"name":    tftypes.NewValue(tftypes.String, "Deploy pipeline"),
	}))

	sent := api.lastRequestBody(t, http.MethodPut)
	expected := []any{map[string]any{"name": "Authorization", "value": "Bearer ci-token"}}
	if sentHeaders, isList := sent["headers"].([]any); !isList || !slices.EqualFunc(sentHeaders, expected, webhookStubHeaderEqual) {
		t.Fatalf("expected the update to send the header with its value, got %#v", sent["headers"])
	}
}

func TestWebhookResource_updateRecordsTheNewFilters(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	updated := protocol.values(t, protocol.apply(t, created, protocol.config(t, map[string]tftypes.Value{
		"project_ids": webhookSet("6a1f2e3d-4c5b-4a69-8877-665544332211"),
		"event_types": webhookSet("targeting_rules.updated"),
	})))

	if got := webhookSortedStrings(t, updated["project_ids"]); !slices.Equal(got, []string{"6a1f2e3d-4c5b-4a69-8877-665544332211"}) {
		t.Fatalf("expected the project filter the API saved, got %v", got)
	}
	if got := webhookSortedStrings(t, updated["event_types"]); !slices.Equal(got, []string{"targeting_rules.updated"}) {
		t.Fatalf("expected the event types the API saved, got %v", got)
	}
	if got := webhookText(t, updated["updated_at"]); got != "2026-10-28T10:01:00.000Z" {
		t.Fatalf("expected the update time the API recorded, got %s", got)
	}
}

func TestWebhookResource_refreshKeepsHeaderValues(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	}))

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := webhookHeaderValues(t, refreshed["headers"]); !maps.Equal(got, map[string]string{"Authorization": "Bearer ci-token"}) {
		t.Fatalf("expected the refresh to keep the header value, got %v", got)
	}
}

func TestWebhookResource_refreshShowsAHeaderAddedOutsideTerraform(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	}))
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["headers"] = []any{map[string]any{"name": "Authorization"}, map[string]any{"name": "X-Team"}}
	})

	refreshed := protocol.values(t, protocol.refresh(t, created))

	expected := map[string]string{"Authorization": "Bearer ci-token", "X-Team": ""}
	if got := webhookHeaderValues(t, refreshed["headers"]); !maps.Equal(got, expected) {
		t.Fatalf("expected the refresh to show the added header without a value, got %v", got)
	}
}

func TestWebhookResource_refreshShowsAHeaderRemovedOutsideTerraform(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	}))
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["headers"] = []any{}
	})

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := webhookHeaderValues(t, refreshed["headers"]); len(got) != 0 {
		t.Fatalf("expected the refresh to show no header, got %v", got)
	}
}

func TestWebhookResource_refreshPicksUpASecretRotatedOutsideTerraform(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["secret"] = "whsec_cm90YXRlZCBzdHViIHNpZ25pbmcgc2VjcmV0"
	})

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := webhookText(t, refreshed["secret"]); got != "whsec_cm90YXRlZCBzdHViIHNpZ25pbmcgc2VjcmV0" {
		t.Fatalf("expected the refresh to pick up the rotated secret, got %s", got)
	}
}

func TestWebhookResource_planEnablesAWebhookDisabledAfterFailedDeliveries(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))
	api.changeOutsideTerraform(func(saved map[string]any) {
		saved["enabled"] = false
		saved["disabledReason"] = "failures"
	})
	refreshed := protocol.refresh(t, created)

	planned := protocol.plannedValues(t, refreshed, protocol.config(t, nil))

	if !webhookBoolean(t, planned["enabled"]) {
		t.Fatal("expected the plan to enable the webhook again")
	}
}

func TestWebhookResource_refreshForgetsAWebhookDeletedOutsideTerraform(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))
	api.deleteOutsideTerraform()

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if refreshed != nil {
		t.Fatalf("a deleted webhook must leave the state so it can be created again, got %v", refreshed)
	}
}

func TestWebhookResource_keepsTheDeclaredNameTheAPITrimmed(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, " CI pipeline "),
	}))

	refreshed := protocol.values(t, protocol.refresh(t, created))

	if got := webhookText(t, protocol.values(t, created)["name"]); got != " CI pipeline " {
		t.Fatalf("expected the create to keep the declared name, got %q", got)
	}
	if got := webhookText(t, refreshed["name"]); got != " CI pipeline " {
		t.Fatalf("expected the refresh to keep the declared name, got %q", got)
	}
}

func TestWebhookResource_importRecoversTheSavedWebhook(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"event_types": webhookSet("targeting_rules.updated"),
	}))

	imported := protocol.values(t, protocol.importByID(t, webhookStubID))

	if got := webhookText(t, imported["name"]); got != "CI pipeline" {
		t.Fatalf("expected the saved name, got %q", got)
	}
	if got := webhookText(t, imported["secret"]); got != webhookStubSecret {
		t.Fatalf("expected the saved secret, got %q", got)
	}
	if got := webhookSortedStrings(t, imported["event_types"]); !slices.Equal(got, []string{"targeting_rules.updated"}) {
		t.Fatalf("expected the saved event types, got %v", got)
	}
}

func TestWebhookResource_importRecoversHeaderNamesWithoutValues(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)
	protocol.apply(t, protocol.absent(t), protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	}))

	imported := protocol.values(t, protocol.importByID(t, webhookStubID))

	if got := webhookHeaderValues(t, imported["headers"]); !maps.Equal(got, map[string]string{"Authorization": ""}) {
		t.Fatalf("expected the header name without its value, got %v", got)
	}
}

func TestWebhookResource_firstApplyAfterImportWritesTheDeclaredHeaderValues(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	configuration := protocol.config(t, map[string]tftypes.Value{
		"headers": webhookHeaders(map[string]string{"Authorization": "Bearer ci-token"}),
	})
	protocol.apply(t, protocol.absent(t), configuration)
	imported := protocol.importByID(t, webhookStubID)

	planned := protocol.plannedValues(t, imported, configuration)
	protocol.apply(t, imported, configuration)

	if got := webhookHeaderValues(t, planned["headers"]); !maps.Equal(got, map[string]string{"Authorization": "Bearer ci-token"}) {
		t.Fatalf("expected the plan after the import to change the header to its declared value, got %v", got)
	}
	sent := api.lastRequestBody(t, http.MethodPut)
	expected := []any{map[string]any{"name": "Authorization", "value": "Bearer ci-token"}}
	if sentHeaders, isList := sent["headers"].([]any); !isList || !slices.EqualFunc(sentHeaders, expected, webhookStubHeaderEqual) {
		t.Fatalf("expected the apply after the import to send the header value, got %#v", sent["headers"])
	}
}

func TestWebhookResource_destroyDeletesTheWebhook(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))

	diagnostics := protocol.destroy(t, created)

	requireWebhookDiagnostics(t, diagnostics)
	if api.holdsAWebhook() {
		t.Fatal("expected the destroy to delete the webhook")
	}
}

func TestWebhookResource_destroySucceedsWhenTheWebhookIsAlreadyGone(t *testing.T) {
	protocol, api := newWebhookStubProtocol(t)
	created := protocol.apply(t, protocol.absent(t), protocol.config(t, nil))
	api.deleteOutsideTerraform()

	diagnostics := protocol.destroy(t, created)

	requireWebhookDiagnostics(t, diagnostics)
}

func TestWebhookResource_rejectsAHeaderNameHTTPDoesNotAllow(t *testing.T) {
	protocol, _ := newWebhookStubProtocol(t)

	validated, err := protocol.server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "configdirector_webhook",
		Config:   protocol.config(t, map[string]tftypes.Value{"headers": webhookHeaders(map[string]string{"X Team": "payments"})}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := webhookErrorAttribute(t, validated.Diagnostics); got != fmt.Sprint(tftypes.AttributeName("headers")) {
		t.Fatalf("expected the error on headers for a name with a space, got it on %s", got)
	}
}

type webhookAcceptance struct {
	webhookProtocol
	api  *client.Client
	name string
}

type webhookSavedByTheAPI struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Headers []struct {
		Name string `json:"name"`
	} `json:"headers"`
	Enabled        bool     `json:"enabled"`
	ProjectIDs     []string `json:"projectIds"`
	EnvironmentIDs []string `json:"environmentIds"`
	TagIDs         []string `json:"tagIds"`
	EventTypes     []string `json:"eventTypes"`
	AlertKinds     []string `json:"alertKinds"`
	Secret         string   `json:"secret"`
}

func newWebhookAcceptance(t *testing.T) webhookAcceptance {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("real API tests require TF_ACC=1")
	}
	testAccPreCheck(t)
	token := os.Getenv("CONFIGDIRECTOR_TOKEN")
	return webhookAcceptance{
		webhookProtocol: newWebhookProtocol(t, testAccBaseURL(), token),
		api:             client.New(testAccBaseURL(), token),
		name:            testAccWebhookNamePrefix + strings.TrimPrefix(newTestAccProject(t).Slug, testAccProjectSlugPrefix),
	}
}

func (acceptance webhookAcceptance) config(t *testing.T, attributes map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	configured := map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, acceptance.name),
		"url":  tftypes.NewValue(tftypes.String, testAccBaseURL()+"/terraform-acceptance-webhook"),
	}
	maps.Copy(configured, attributes)
	return acceptance.webhookProtocol.config(t, configured)
}

func (acceptance webhookAcceptance) create(t *testing.T, configuration *tfprotov6.DynamicValue) *tfprotov6.DynamicValue {
	t.Helper()

	created := acceptance.apply(t, acceptance.absent(t), configuration)
	id := webhookText(t, acceptance.values(t, created)["id"])
	t.Cleanup(func() {
		err := acceptance.api.DeleteWebhook(context.Background(), id)
		var apiError *client.APIError
		alreadyDeleted := errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
		if err != nil && !alreadyDeleted {
			t.Errorf("deleting test webhook: %s", err)
		}
	})
	return created
}

func (acceptance webhookAcceptance) project(t *testing.T) *client.Project {
	t.Helper()

	fixture := newTestAccProject(t)
	project, err := acceptance.api.CreateProject(context.Background(), client.CreateProjectRequest{Name: fixture.Name, Slug: fixture.Slug})
	if err != nil {
		t.Fatalf("creating test project: %s", err)
	}
	t.Cleanup(func() {
		if err := acceptance.api.DeleteProject(context.Background(), project.ID); err != nil {
			t.Errorf("deleting test project: %s", err)
		}
	})
	return project
}

func (acceptance webhookAcceptance) requestSaved(t *testing.T, state *tfprotov6.DynamicValue) *http.Response {
	t.Helper()

	id := webhookText(t, acceptance.values(t, state)["id"])
	request, err := http.NewRequest(http.MethodGet, acceptance.api.BaseURL+"/v1/webhooks/"+id, nil)
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

func (acceptance webhookAcceptance) saved(t *testing.T, state *tfprotov6.DynamicValue) webhookSavedByTheAPI {
	t.Helper()

	response := acceptance.requestSaved(t, state)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reading the saved webhook: HTTP %d", response.StatusCode)
	}
	var saved webhookSavedByTheAPI
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	slices.Sort(saved.EventTypes)
	slices.Sort(saved.AlertKinds)
	return saved
}

func TestAccWebhookResource_createsTheWebhook(t *testing.T) {
	acceptance := newWebhookAcceptance(t)

	created := acceptance.create(t, acceptance.config(t, map[string]tftypes.Value{
		"headers":     webhookHeaders(map[string]string{"X-Receiver-Token": "acceptance-token"}),
		"alert_kinds": webhookSet("config-settled"),
	}))

	saved := acceptance.saved(t, created)
	if saved.Name != acceptance.name || saved.URL != testAccBaseURL()+"/terraform-acceptance-webhook" || !saved.Enabled {
		t.Fatalf("expected an enabled webhook with the declared name and URL, the API has %+v", saved)
	}
	if len(saved.Headers) != 1 || saved.Headers[0].Name != "X-Receiver-Token" {
		t.Fatalf("expected the X-Receiver-Token header, the API has %+v", saved.Headers)
	}
	if !slices.Equal(saved.EventTypes, changeEventTypesTheAPIOffers) || !slices.Equal(saved.AlertKinds, []string{"config-settled"}) {
		t.Fatalf("expected every change event type and the config settled alert, the API has %v and %v", saved.EventTypes, saved.AlertKinds)
	}
	if got := webhookText(t, acceptance.values(t, created)["secret"]); got == "" || got != saved.Secret {
		t.Fatal("expected the state to hold the secret the API generated")
	}
}

func TestAccWebhookResource_updatesTheFilters(t *testing.T) {
	acceptance := newWebhookAcceptance(t)
	project := acceptance.project(t)
	environment, err := acceptance.api.CreateEnvironment(context.Background(), project.ID, client.CreateEnvironmentRequest{
		Name: "Production", Slug: "production-webhook", Color: "red", Live: true,
	})
	if err != nil {
		t.Fatalf("creating test environment: %s", err)
	}
	tag, err := acceptance.api.CreateTag(context.Background(), project.ID, client.TagNameRequest{Name: "Payments"})
	if err != nil {
		t.Fatalf("creating test tag: %s", err)
	}
	created := acceptance.create(t, acceptance.config(t, nil))

	updated := acceptance.apply(t, created, acceptance.config(t, map[string]tftypes.Value{
		"project_ids":     webhookSet(project.ID),
		"environment_ids": webhookSet(environment.ID),
		"tag_ids":         webhookSet(tag.ID),
		"event_types":     webhookSet("targeting_rules.updated"),
		"alert_kinds":     webhookSet("config-type-mismatch", "default-value-mismatch"),
	}))

	saved := acceptance.saved(t, updated)
	if !slices.Equal(saved.ProjectIDs, []string{project.ID}) || !slices.Equal(saved.EnvironmentIDs, []string{environment.ID}) || !slices.Equal(saved.TagIDs, []string{tag.ID}) {
		t.Fatalf("expected the project, environment and tag filters, the API has %v, %v and %v", saved.ProjectIDs, saved.EnvironmentIDs, saved.TagIDs)
	}
	if !slices.Equal(saved.EventTypes, []string{"targeting_rules.updated"}) || !slices.Equal(saved.AlertKinds, []string{"config-type-mismatch", "default-value-mismatch"}) {
		t.Fatalf("expected the targeting rules event type and both alert kinds, the API has %v and %v", saved.EventTypes, saved.AlertKinds)
	}
	if got := webhookText(t, acceptance.values(t, updated)["secret"]); got != webhookText(t, acceptance.values(t, created)["secret"]) {
		t.Fatal("expected the update to keep the secret")
	}
}

func TestAccWebhookResource_importsByID(t *testing.T) {
	acceptance := newWebhookAcceptance(t)
	created := acceptance.create(t, acceptance.config(t, map[string]tftypes.Value{
		"headers":     webhookHeaders(map[string]string{"X-Receiver-Token": "acceptance-token"}),
		"event_types": webhookSet("config.created", "config.updated"),
	}))

	imported := acceptance.values(t, acceptance.importByID(t, webhookText(t, acceptance.values(t, created)["id"])))

	if got := webhookText(t, imported["name"]); got != acceptance.name {
		t.Fatalf("expected the imported name %q, got %q", acceptance.name, got)
	}
	if got := webhookSortedStrings(t, imported["event_types"]); !slices.Equal(got, []string{"config.created", "config.updated"}) {
		t.Fatalf("expected the imported event types, got %v", got)
	}
	if got := webhookHeaderValues(t, imported["headers"]); !maps.Equal(got, map[string]string{"X-Receiver-Token": ""}) {
		t.Fatalf("expected the imported header name without its value, got %v", got)
	}
	if got := webhookText(t, imported["secret"]); got != webhookText(t, acceptance.values(t, created)["secret"]) {
		t.Fatal("expected the import to recover the secret")
	}
}

func TestAccWebhookResource_destroysTheWebhook(t *testing.T) {
	acceptance := newWebhookAcceptance(t)
	created := acceptance.create(t, acceptance.config(t, nil))

	requireWebhookDiagnostics(t, acceptance.destroy(t, created))

	if got := acceptance.requestSaved(t, created).StatusCode; got != http.StatusNotFound {
		t.Fatalf("expected the API to no longer have the webhook, got HTTP %d", got)
	}
}
