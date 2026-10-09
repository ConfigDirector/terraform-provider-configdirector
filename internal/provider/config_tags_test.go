package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	ctymsgpack "github.com/zclconf/go-cty/cty/msgpack"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

type configTagsTest struct {
	tagTestResource
	server tfprotov6.ProviderServer
	typeOf tftypes.Type
}

type configTagsState struct {
	value   *tfprotov6.DynamicValue
	private []byte
}

func configTagsServer(t *testing.T) (tfprotov6.ProviderServer, *tfprotov6.GetProviderSchemaResponse) {
	t.Helper()

	server := providerserver.NewProtocol6(New("test")())()
	definition, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, definition.Diagnostics)
	return server, definition
}

func newConfigTagsTest(t *testing.T) configTagsTest {
	t.Helper()

	fixture := newTagTestResource(t)
	server, definition := configTagsServer(t)
	providerType := definition.Provider.ValueType()
	configured, err := server.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		Config: configTagsDynamic(t, tftypes.NewValue(providerType, map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, nil), "base_url": tftypes.NewValue(tftypes.String, nil),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, configured.Diagnostics)
	return configTagsTest{tagTestResource: fixture, server: server, typeOf: definition.ResourceSchemas["configdirector_config"].ValueType()}
}

func requireConfigTagsDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("unexpected diagnostic: %s: %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
}

func configTagsDynamic(t *testing.T, value tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	dynamic, err := tfprotov6.NewDynamicValue(value.Type(), value)
	if err != nil {
		t.Fatal(err)
	}
	return &dynamic
}

func configTagsNames(names ...string) tftypes.Value {
	values := make([]tftypes.Value, len(names))
	for i, name := range names {
		values[i] = tftypes.NewValue(tftypes.String, name)
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, values)
}

func (fixture configTagsTest) config(t *testing.T, tags tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()

	attributes := fixture.typeOf.(tftypes.Object).AttributeTypes
	if _, exists := attributes["tags"]; !exists {
		t.Fatal("config resources must accept tags as an unordered set of names")
	}
	values := make(map[string]tftypes.Value, len(attributes))
	for name, typeOf := range attributes {
		values[name] = tftypes.NewValue(typeOf, nil)
	}
	values["project_id"] = tftypes.NewValue(tftypes.String, fixture.project.ID)
	values["key"] = tftypes.NewValue(tftypes.String, "checkout-flag")
	values["role"] = tftypes.NewValue(tftypes.String, "flag")
	values["lifetime"] = tftypes.NewValue(tftypes.String, "temporary")
	values["type"] = tftypes.NewValue(tftypes.String, "boolean")
	values["initial_value"] = tftypes.NewValue(tftypes.Bool, false)
	values["tags"] = tags
	return configTagsDynamic(t, tftypes.NewValue(fixture.typeOf, values))
}

func (fixture configTagsTest) change(t *testing.T, prior configTagsState, configuration *tfprotov6.DynamicValue) configTagsState {
	t.Helper()

	plan := fixture.plan(t, prior, configuration)
	applied, err := fixture.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_config", PriorState: prior.value, Config: configuration,
		PlannedState: plan.PlannedState, PlannedPrivate: plan.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, applied.Diagnostics)
	return configTagsState{value: applied.NewState, private: applied.Private}
}

func (fixture configTagsTest) plan(t *testing.T, prior configTagsState, configuration *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	t.Helper()

	if prior.value == nil {
		prior.value = configTagsDynamic(t, tftypes.NewValue(fixture.typeOf, nil))
	}
	proposed, err := configuration.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]tftypes.Value
	if err := proposed.As(&values); err != nil {
		t.Fatal(err)
	}
	previous, err := prior.value.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	if !previous.IsNull() {
		var previousValues map[string]tftypes.Value
		if err := previous.As(&previousValues); err != nil {
			t.Fatal(err)
		}
		for name, value := range values {
			if value.IsNull() {
				values[name] = previousValues[name]
			}
		}
	}
	planned, err := fixture.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: "configdirector_config", PriorState: prior.value, Config: configuration, PriorPrivate: prior.private,
		ProposedNewState: configTagsDynamic(t, tftypes.NewValue(fixture.typeOf, values)),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, planned.Diagnostics)
	return planned
}

func (fixture configTagsTest) refresh(t *testing.T, state configTagsState) configTagsState {
	t.Helper()

	read, err := fixture.server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: "configdirector_config", CurrentState: state.value, Private: state.private,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, read.Diagnostics)
	return configTagsState{value: read.NewState, private: read.Private}
}

func (fixture configTagsTest) names(t *testing.T, state configTagsState) []string {
	t.Helper()

	typeJSON, err := fixture.typeOf.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	terraformType, err := ctyjson.UnmarshalType(typeJSON)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ctymsgpack.Unmarshal(state.value.MsgPack, terraformType)
	if err != nil {
		t.Fatal(err)
	}
	tags := value.GetAttr("tags")
	names := make([]string, 0, tags.LengthInt())
	for iterator := tags.ElementIterator(); iterator.Next(); {
		_, name := iterator.Element()
		names = append(names, name.AsString())
	}
	slices.Sort(names)
	return names
}

func (fixture configTagsTest) assignedNames(t *testing.T) []string {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, fixture.api.BaseURL+"/v1/projects/"+fixture.project.ID+"/configs/checkout-flag", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+fixture.api.Token)
	response, err := fixture.api.HTTPClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reading persisted assignments: HTTP %d", response.StatusCode)
	}
	var config struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(config.Tags))
	for i, tag := range config.Tags {
		names[i] = tag.Name
	}
	slices.Sort(names)
	return names
}

func (fixture configTagsTest) catalogTag(t *testing.T, name string) *client.Tag {
	t.Helper()

	tag, err := fixture.api.CreateTag(context.Background(), fixture.project.ID, client.TagNameRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return tag
}

func TestAccConfigTags_createByName(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments / EU & US + %2F")
	configuration := fixture.config(t, configTagsNames("Payments / EU & US + %2F"))

	state := fixture.change(t, configTagsState{}, configuration)

	if got := fixture.names(t, state); !slices.Equal(got, []string{"Payments / EU & US + %2F"}) {
		t.Fatalf("assigned state names: %v", got)
	}
	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Payments / EU & US + %2F"}) {
		t.Fatalf("persisted assigned names: %v", got)
	}
}

func TestAccConfigTags_clearAll(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))

	cleared := fixture.change(t, created, fixture.config(t, configTagsNames()))

	if got := fixture.assignedNames(t); len(got) != 0 {
		t.Fatalf("explicit empty tags must clear persisted assignments: %v", got)
	}
	if got := fixture.names(t, cleared); len(got) != 0 {
		t.Fatalf("cleared state tags: %v", got)
	}
}

func TestAccConfigTags_preserveEquivalentSpelling(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	configuration := fixture.config(t, configTagsNames("PAYMENTS", " payments "))

	created := fixture.change(t, configTagsState{}, configuration)
	refreshed := fixture.refresh(t, created)

	if got := fixture.names(t, refreshed); !slices.Equal(got, []string{" payments ", "PAYMENTS"}) {
		t.Fatalf("declared spelling must survive refresh: %v", got)
	}
	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Payments"}) {
		t.Fatalf("case-equivalent names must assign one catalog identity: %v", got)
	}
	planned := fixture.plan(t, refreshed, configuration)
	plannedValue, err := planned.PlannedState.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	refreshedValue, err := refreshed.value.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	if !plannedValue.Equal(refreshedValue) {
		t.Fatal("equivalent names must not cause repeated drift")
	}
}

func (fixture configTagsTest) assign(t *testing.T, ids ...string) {
	t.Helper()

	if _, err := fixture.api.UpdateConfig(context.Background(), fixture.project.ID, "checkout-flag", client.UpdateConfigRequest{
		Key: "checkout-flag", Role: "flag", Lifetime: "temporary", Type: "boolean", Server: true, Client: true, TagIDs: &ids,
	}); err != nil {
		t.Fatal(err)
	}
}

func (fixture configTagsTest) withDescription(t *testing.T, configuration *tfprotov6.DynamicValue, description string) *tfprotov6.DynamicValue {
	t.Helper()

	value, err := configuration.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]tftypes.Value
	if err := value.As(&attributes); err != nil {
		t.Fatal(err)
	}
	attributes["description"] = tftypes.NewValue(tftypes.String, description)
	return configTagsDynamic(t, tftypes.NewValue(fixture.typeOf, attributes))
}

func (fixture configTagsTest) importConfig(t *testing.T) configTagsState {
	t.Helper()

	imported, err := fixture.server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{
		TypeName: "configdirector_config", ID: fixture.project.ID + "/checkout-flag",
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, imported.Diagnostics)
	if len(imported.ImportedResources) != 1 {
		t.Fatalf("expected one imported config, got %d", len(imported.ImportedResources))
	}
	resource := imported.ImportedResources[0]
	return fixture.refresh(t, configTagsState{value: resource.State, private: resource.Private})
}

func TestAccConfigTags_omissionAfterImportPreservesLatestAssignments(t *testing.T) {
	fixture := newConfigTagsTest(t)
	payments := fixture.catalogTag(t, "Payments")
	checkout := fixture.catalogTag(t, "Checkout")
	omitted := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil)
	fixture.change(t, configTagsState{}, fixture.config(t, omitted))
	fixture.assign(t, payments.ID)
	imported := fixture.importConfig(t)
	if got := fixture.names(t, imported); !slices.Equal(got, []string{"Payments"}) {
		t.Fatalf("imported assigned names: %v", got)
	}
	fixture.assign(t, checkout.ID)
	configuration := fixture.withDescription(t, fixture.config(t, omitted), "Unrelated description edit")

	updated := fixture.change(t, imported, configuration)

	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("omitted tags must preserve latest remote assignments: %v", got)
	}
	if got := fixture.names(t, updated); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("updated observed names: %v", got)
	}
}

func TestAccConfigTags_singleDataSourceExposesAssignedNames(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))

	definition, err := fixture.server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typeOf := definition.DataSourceSchemas["configdirector_config"].ValueType()
	attributes := typeOf.(tftypes.Object).AttributeTypes
	if _, exists := attributes["tags"]; !exists {
		t.Fatal("config data source must expose assigned tag names")
	}
	values := make(map[string]tftypes.Value, len(attributes))
	for name, typeOf := range attributes {
		values[name] = tftypes.NewValue(typeOf, nil)
	}
	values["project_id"] = tftypes.NewValue(tftypes.String, fixture.project.ID)
	values["key"] = tftypes.NewValue(tftypes.String, "checkout-flag")
	read, err := fixture.server.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{
		TypeName: "configdirector_config", Config: configTagsDynamic(t, tftypes.NewValue(typeOf, values)),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, read.Diagnostics)
	value, err := read.State.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.As(&values); err != nil {
		t.Fatal(err)
	}
	if !values["tags"].Equal(configTagsNames("Payments")) {
		t.Fatalf("data-source assigned names: %s", values["tags"])
	}
}

func TestAccConfigTags_listDataSourceExposesAssignedNames(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))

	definition, err := fixture.server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typeOf := definition.DataSourceSchemas["configdirector_configs"].ValueType()
	read, err := fixture.server.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{
		TypeName: "configdirector_configs", Config: configTagsDynamic(t, tftypes.NewValue(typeOf, map[string]tftypes.Value{
			"project_id": tftypes.NewValue(tftypes.String, fixture.project.ID),
			"configs":    tftypes.NewValue(typeOf.(tftypes.Object).AttributeTypes["configs"], nil),
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, read.Diagnostics)
	value, err := read.State.Unmarshal(typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]tftypes.Value
	if err := value.As(&attributes); err != nil {
		t.Fatal(err)
	}
	var configs []tftypes.Value
	if err := attributes["configs"].As(&configs); err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected one config, got %d", len(configs))
	}
	if err := configs[0].As(&attributes); err != nil {
		t.Fatal(err)
	}
	if _, exists := attributes["tags"]; !exists {
		t.Fatal("configs data source must expose assigned tag names")
	}
	if !attributes["tags"].Equal(configTagsNames("Payments")) {
		t.Fatalf("listed assigned names: %s", attributes["tags"])
	}
}

func TestAccConfigTags_reconcileAssignmentDrift(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	checkout := fixture.catalogTag(t, "Checkout")
	configuration := fixture.config(t, configTagsNames("Payments"))
	created := fixture.change(t, configTagsState{}, configuration)
	fixture.assign(t, checkout.ID)

	refreshed := fixture.refresh(t, created)

	if got := fixture.names(t, refreshed); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("refresh must detect assignment drift: %v", got)
	}
	fixture.change(t, refreshed, configuration)
	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Payments"}) {
		t.Fatalf("declared assignments must replace remote drift: %v", got)
	}
}

func TestAccConfigTags_relinquishOwnership(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	checkout := fixture.catalogTag(t, "Checkout")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))
	fixture.assign(t, checkout.ID)
	omitted := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil)
	configuration := fixture.withDescription(t, fixture.config(t, omitted), "Assignments are now managed remotely")

	updated := fixture.change(t, created, configuration)

	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("removing tags must relinquish assignment ownership: %v", got)
	}
	if got := fixture.names(t, updated); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("observed names after relinquishing: %v", got)
	}
}

func TestAccConfigTags_managedNameReferenceFollowsRename(t *testing.T) {
	fixture := newConfigTagsTest(t)
	tag := fixture.tagTestResource.create(t, "Payments")
	id := tagAttribute(t, tag, "id")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames(tagAttribute(t, tag, "name"))))
	renamed := fixture.tagTestResource.update(t, tag, "Checkout")

	updated := fixture.change(t, created, fixture.config(t, configTagsNames(tagAttribute(t, renamed, "name"))))

	if got := tagAttribute(t, renamed, "id"); got != id {
		t.Fatalf("rename changed tag identity: %s", got)
	}
	if got := fixture.names(t, updated); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("name reference must follow renamed catalog name: %v", got)
	}
	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("persisted renamed assignment: %v", got)
	}
}

func TestAccConfigTags_unknownNameDoesNotCreateTag(t *testing.T) {
	fixture := newConfigTagsTest(t)
	configuration := fixture.config(t, configTagsNames("Missing / Payments"))
	plan := fixture.plan(t, configTagsState{}, configuration)

	applied, err := fixture.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_config", Config: configuration, PlannedState: plan.PlannedState, PlannedPrivate: plan.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Diagnostics) == 0 {
		t.Fatal("unknown name must fail before creating the config")
	}
	diagnostic := applied.Diagnostics[0]
	if diagnostic.Severity != tfprotov6.DiagnosticSeverityError || !strings.Contains(diagnostic.Detail, "Missing / Payments") || !strings.Contains(diagnostic.Detail, fixture.project.ID) {
		t.Fatalf("unknown name diagnostic must identify name and project: %v", diagnostic)
	}
	if !strings.Contains(diagnostic.Detail, "configdirector_tag") {
		t.Fatalf("missing-name diagnostic must explain how to create the catalog entry: %s", diagnostic.Detail)
	}
	_, tagError := fixture.api.GetTagByName(context.Background(), fixture.project.ID, "Missing / Payments")
	var tagAPIError *client.APIError
	if !errors.As(tagError, &tagAPIError) || tagAPIError.StatusCode != http.StatusNotFound {
		t.Fatalf("assignments must not create unknown tags implicitly; expected HTTP 404, got %v", tagError)
	}
	_, configError := fixture.api.GetConfig(context.Background(), fixture.project.ID, "checkout-flag")
	var configAPIError *client.APIError
	if !errors.As(configError, &configAPIError) || configAPIError.StatusCode != http.StatusNotFound {
		t.Fatalf("failed resolution must not create a config; expected HTTP 404, got %v", configError)
	}
}

func TestConfigTags_unknownReferenceRemainsUnknown(t *testing.T) {
	server, definition := configTagsServer(t)
	fixture := configTagsTest{server: server, typeOf: definition.ResourceSchemas["configdirector_config"].ValueType(), tagTestResource: tagTestResource{project: &client.Project{ID: "01234567-89ab-4cde-8f01-23456789abcd"}}}
	unknown := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, tftypes.UnknownValue)
	configuration := fixture.config(t, unknown)

	planned := fixture.plan(t, configTagsState{}, configuration)

	value, err := planned.PlannedState.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]tftypes.Value
	if err := value.As(&attributes); err != nil {
		t.Fatal(err)
	}
	if attributes["tags"].IsKnown() {
		t.Fatalf("unknown tag reference must remain unknown, got %s", attributes["tags"])
	}
}

func TestConfigTags_unknownNameElementRemainsUnknown(t *testing.T) {
	server, definition := configTagsServer(t)
	fixture := configTagsTest{server: server, typeOf: definition.ResourceSchemas["configdirector_config"].ValueType(), tagTestResource: tagTestResource{project: &client.Project{ID: "01234567-89ab-4cde-8f01-23456789abcd"}}}
	unknown := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	configuration := fixture.config(t, unknown)

	planned := fixture.plan(t, configTagsState{}, configuration)

	value, err := planned.PlannedState.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]tftypes.Value
	if err := value.As(&attributes); err != nil {
		t.Fatal(err)
	}
	if attributes["tags"].IsFullyKnown() {
		t.Fatalf("unknown managed name must not become an empty assignment set: %s", attributes["tags"])
	}
}

func TestAccConfigTags_reassignAfterOmission(t *testing.T) {
	fixture := newConfigTagsTest(t)
	payments := fixture.catalogTag(t, "Payments")
	fixture.catalogTag(t, "Checkout")
	omitted := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil)
	created := fixture.change(t, configTagsState{}, fixture.config(t, omitted))
	fixture.assign(t, payments.ID)
	refreshed := fixture.refresh(t, created)

	updated := fixture.change(t, refreshed, fixture.config(t, configTagsNames("Checkout")))

	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("explicit tags must take assignment ownership: %v", got)
	}
	if got := fixture.names(t, updated); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("updated assigned names: %v", got)
	}
}

func TestAccConfigTags_caseAndOrderUpdateKeepsIdentity(t *testing.T) {
	fixture := newConfigTagsTest(t)
	payments := fixture.catalogTag(t, "Payments")
	checkout := fixture.catalogTag(t, "Checkout")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments", "Checkout")))
	configuration := fixture.config(t, configTagsNames("checkout", "PAYMENTS"))

	updated := fixture.change(t, created, configuration)
	refreshed := fixture.refresh(t, updated)

	if got := fixture.names(t, refreshed); !slices.Equal(got, []string{"PAYMENTS", "checkout"}) {
		t.Fatalf("updated declared spelling must survive refresh: %v", got)
	}
	config, err := fixture.api.GetConfig(context.Background(), fixture.project.ID, "checkout-flag")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(config.Tags))
	for i, tag := range config.Tags {
		ids[i] = tag.ID
	}
	if len(ids) != 2 || !slices.Contains(ids, payments.ID) || !slices.Contains(ids, checkout.ID) {
		t.Fatalf("casing/order must preserve catalog identities: %v", ids)
	}
}

func TestAccConfigTags_deletedTagDetachesAssignment(t *testing.T) {
	fixture := newConfigTagsTest(t)
	payments := fixture.catalogTag(t, "Payments")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))
	if err := fixture.api.DeleteTag(context.Background(), fixture.project.ID, payments.ID); err != nil {
		t.Fatal(err)
	}

	refreshed := fixture.refresh(t, created)

	if got := fixture.names(t, refreshed); len(got) != 0 {
		t.Fatalf("deleted catalog tag must disappear from assignments: %v", got)
	}
	if got := fixture.assignedNames(t); len(got) != 0 {
		t.Fatalf("persisted assignments must exclude deleted tag: %v", got)
	}
}

func TestAccConfigTags_adoptedPlanNamesAreNotManagedInput(t *testing.T) {
	fixture := newConfigTagsTest(t)
	fixture.catalogTag(t, "Payments")
	checkout := fixture.catalogTag(t, "Checkout")
	created := fixture.change(t, configTagsState{}, fixture.config(t, configTagsNames("Payments")))
	fixture.assign(t, checkout.ID)
	omitted := tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, nil)
	configuration := fixture.withDescription(t, fixture.config(t, omitted), "Keep assignment ownership remote")
	plan := fixture.plan(t, created, configuration)
	value, err := plan.PlannedState.Unmarshal(fixture.typeOf)
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]tftypes.Value
	if err := value.As(&attributes); err != nil {
		t.Fatal(err)
	}
	attributes["tags"] = configTagsNames("Payments")

	applied, err := fixture.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "configdirector_config", PriorState: created.value, Config: configuration,
		PlannedState: configTagsDynamic(t, tftypes.NewValue(fixture.typeOf, attributes)), PlannedPrivate: plan.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireConfigTagsDiagnostics(t, applied.Diagnostics)

	if got := fixture.assignedNames(t); !slices.Equal(got, []string{"Checkout"}) {
		t.Fatalf("adopted names must never override omitted configuration: %v", got)
	}
}

func TestAccConfigTags_renamedSpellingCannotHideAddedAssignment(t *testing.T) {
	fixture := newConfigTagsTest(t)
	original := fixture.catalogTag(t, "Payments")
	configuration := fixture.config(t, configTagsNames("Payments"))
	created := fixture.change(t, configTagsState{}, configuration)
	if _, err := fixture.api.UpdateTag(context.Background(), fixture.project.ID, original.ID, client.TagNameRequest{Name: "Checkout"}); err != nil {
		t.Fatal(err)
	}
	payments := fixture.catalogTag(t, "Payments")
	fixture.assign(t, original.ID, payments.ID)

	refreshed := fixture.refresh(t, created)

	if got := fixture.names(t, refreshed); !slices.Equal(got, []string{"Checkout", "Payments"}) {
		t.Fatalf("declared spelling must not hide an added assignment after a rename: %v", got)
	}
	fixture.change(t, refreshed, configuration)
	config, err := fixture.api.GetConfig(context.Background(), fixture.project.ID, "checkout-flag")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Tags) != 1 || config.Tags[0].ID != payments.ID {
		t.Fatalf("reconciliation must retain only the catalog identity matching the declared name: %v", config.Tags)
	}
}
