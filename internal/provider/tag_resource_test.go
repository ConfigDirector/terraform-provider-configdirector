package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

type tagTestValues struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
}

type tagTestResource struct {
	resource.Resource
	schema  schema.Schema
	api     *client.Client
	project *client.Project
}

func tagResourceForTest(t *testing.T) resource.Resource {
	t.Helper()

	for _, factory := range New("test")().Resources(context.Background()) {
		candidate := factory()
		var metadata resource.MetadataResponse
		candidate.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "configdirector"}, &metadata)
		if metadata.TypeName == "configdirector_tag" {
			return candidate
		}
	}
	t.Fatal("configdirector_tag must be registered as a provider resource")
	return nil
}

func newTagTestResource(t *testing.T) tagTestResource {
	t.Helper()

	fixture := newTestAccProject(t)
	fixture.Slug = "tf-tag-" + strings.TrimPrefix(fixture.Slug, testAccProjectSlugPrefix)
	return newTagTestResourceWithProject(t, fixture)
}

func newTagTestResourceWithProject(t *testing.T, fixture testAccProject) tagTestResource {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("real API tests require TF_ACC=1")
	}
	testAccPreCheck(t)
	tag := tagResourceForTest(t)
	api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
	project, err := api.CreateProject(context.Background(), client.CreateProjectRequest{Name: fixture.Name, Slug: fixture.Slug})
	if err != nil {
		t.Fatalf("creating test project: %s", err)
	}
	t.Cleanup(func() {
		if err := api.DeleteProject(context.Background(), project.ID); err != nil {
			t.Errorf("deleting test project: %s", err)
		}
	})

	var configuration resource.ConfigureResponse
	tag.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: api}, &configuration)
	requireTagDiagnostics(t, configuration.Diagnostics)
	var definition resource.SchemaResponse
	tag.Schema(context.Background(), resource.SchemaRequest{}, &definition)
	requireTagDiagnostics(t, definition.Diagnostics)

	return tagTestResource{Resource: tag, schema: definition.Schema, api: api, project: project}
}

func requireTagDiagnostics(t *testing.T, diagnostics diag.Diagnostics) {
	t.Helper()

	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", diagnostics)
	}
}

func (tag tagTestResource) plan(t *testing.T, name string, id types.String) tfsdk.Plan {
	t.Helper()

	plan := tfsdk.Plan{Schema: tag.schema}
	requireTagDiagnostics(t, plan.Set(context.Background(), tagTestValues{
		ID: id, ProjectID: types.StringValue(tag.project.ID), Name: types.StringValue(name),
	}))
	return plan
}

func (tag tagTestResource) create(t *testing.T, name string) tfsdk.State {
	t.Helper()

	plan := tag.plan(t, name, types.StringUnknown())
	response := resource.CreateResponse{State: tfsdk.State{Schema: tag.schema, Raw: plan.Raw}}
	tag.Create(context.Background(), resource.CreateRequest{Plan: plan}, &response)
	requireTagDiagnostics(t, response.Diagnostics)
	return response.State
}

func (tag tagTestResource) read(t *testing.T, state tfsdk.State) tfsdk.State {
	t.Helper()

	response := resource.ReadResponse{State: state}
	tag.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	requireTagDiagnostics(t, response.Diagnostics)
	return response.State
}

func (tag tagTestResource) update(t *testing.T, state tfsdk.State, name string) tfsdk.State {
	t.Helper()

	plan := tag.plan(t, name, types.StringValue(tagAttribute(t, state, "id")))
	response := resource.UpdateResponse{State: tfsdk.State{Schema: tag.schema, Raw: plan.Raw}}
	tag.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &response)
	requireTagDiagnostics(t, response.Diagnostics)
	return response.State
}

func (tag tagTestResource) importTag(t *testing.T, identifier string) tfsdk.State {
	t.Helper()

	response := resource.ImportStateResponse{State: tfsdk.State{
		Schema: tag.schema,
		Raw:    tftypes.NewValue(tag.schema.Type().TerraformType(context.Background()), nil),
	}}
	tag.Resource.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: identifier}, &response)
	requireTagDiagnostics(t, response.Diagnostics)
	return response.State
}

func tagAttribute(t *testing.T, state tfsdk.State, name string) string {
	t.Helper()

	var value types.String
	requireTagDiagnostics(t, state.GetAttribute(context.Background(), path.Root(name), &value))
	if value.IsNull() || value.IsUnknown() {
		t.Fatalf("expected %s to be known, got %s", name, value)
	}
	return value.ValueString()
}

func TestAccTagResource_createAndRead(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments / EU & US + 100%")
	id := tagAttribute(t, created, "id")
	if id == "" {
		t.Fatal("created tag must have a stable ID")
	}

	saved := tag.read(t, created)
	if got := tagAttribute(t, saved, "name"); got != "Payments / EU & US + 100%" {
		t.Fatalf("expected persisted name Payments / EU & US + 100%%, got %q", got)
	}
	if got := tagAttribute(t, saved, "project_id"); got != tag.project.ID {
		t.Fatalf("expected owning project %q, got %q", tag.project.ID, got)
	}
	if got := tagAttribute(t, saved, "id"); got != id {
		t.Fatalf("expected stable ID %q, got %q", id, got)
	}
}

func TestTagResource_rejectsImportWithoutProjectSeparator(t *testing.T) {
	tag := tagResourceForTest(t)
	importer, ok := tag.(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("configdirector_tag must support import")
	}
	var definition resource.SchemaResponse
	tag.Schema(context.Background(), resource.SchemaRequest{}, &definition)
	response := resource.ImportStateResponse{State: tfsdk.State{
		Schema: definition.Schema,
		Raw:    tftypes.NewValue(definition.Schema.Type().TerraformType(context.Background()), nil),
	}}

	importer.ImportState(context.Background(), resource.ImportStateRequest{ID: "Payments"}, &response)

	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Detail(), "project_id_or_slug/tag_name") {
		t.Fatalf("expected import format diagnostic, got %s", response.Diagnostics)
	}
}

func validateTagNameForTest(t *testing.T, name types.String) diag.Diagnostics {
	t.Helper()

	tag := tagResourceForTest(t)
	validator, ok := tag.(resource.ResourceWithValidateConfig)
	if !ok {
		t.Fatal("configdirector_tag must validate tag names before calling the API")
	}
	var definition resource.SchemaResponse
	tag.Schema(context.Background(), resource.SchemaRequest{}, &definition)
	plan := tfsdk.Plan{Schema: definition.Schema}
	requireTagDiagnostics(t, plan.Set(context.Background(), tagTestValues{
		ID: types.StringNull(), ProjectID: types.StringValue("project-id"), Name: name,
	}))
	var response resource.ValidateConfigResponse

	validator.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{
		Schema: definition.Schema, Raw: plan.Raw,
	}}, &response)
	return response.Diagnostics
}

func TestTagResource_rejectsBlankName(t *testing.T) {
	diagnostics := validateTagNameForTest(t, types.StringValue(" \t\n "))

	if !diagnostics.HasError() || !strings.Contains(diagnostics[0].Detail(), "1–50") {
		t.Fatalf("expected an actionable invalid-name diagnostic, got %s", diagnostics)
	}
}

func TestAccTagResource_renamesInPlace(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	id := tagAttribute(t, created, "id")

	updated := tag.update(t, created, "Checkout")
	saved := tag.read(t, updated)

	if got := tagAttribute(t, saved, "name"); got != "Checkout" {
		t.Fatalf("expected persisted renamed tag Checkout, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got != id {
		t.Fatalf("expected rename to preserve ID %q, got %q", id, got)
	}
}

func TestAccTagResource_deletesTag(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	var deletion resource.DeleteResponse

	tag.Delete(context.Background(), resource.DeleteRequest{State: created}, &deletion)
	requireTagDiagnostics(t, deletion.Diagnostics)
	response := resource.ReadResponse{State: created}
	tag.Read(context.Background(), resource.ReadRequest{State: created}, &response)

	requireTagDiagnostics(t, response.Diagnostics)
	if !response.State.Raw.IsNull() {
		t.Fatal("a deleted tag must be removed from state so it can be recreated")
	}
}

func TestAccTagResource_importByProjectIDPreservesLiteralName(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments / EU & US + %2F")
	id := tagAttribute(t, created, "id")

	imported := tag.importTag(t, tag.project.ID+"/Payments / EU & US + %2F")
	saved := tag.read(t, imported)

	if got := tagAttribute(t, saved, "name"); got != "Payments / EU & US + %2F" {
		t.Fatalf("expected literal name Payments / EU & US + %%2F, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got != id {
		t.Fatalf("expected import to resolve ID %q, got %q", id, got)
	}
	if got := tagAttribute(t, saved, "project_id"); got != tag.project.ID {
		t.Fatalf("expected imported owning project %q, got %q", tag.project.ID, got)
	}
}

func TestAccTagResource_importByProjectSlugUsesAPINameEquivalence(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")

	imported := tag.importTag(t, tag.project.Slug+"/ payments ")
	saved := tag.read(t, imported)

	if got := tagAttribute(t, saved, "name"); got != "Payments" {
		t.Fatalf("expected canonical display name Payments, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got != tagAttribute(t, created, "id") {
		t.Fatalf("expected import to keep the existing tag ID, got %q", got)
	}
}

func TestAccTagResource_readsImportedIdentityAfterRemoteRename(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	id := tagAttribute(t, created, "id")
	imported := tag.importTag(t, tag.project.ID+"/Payments")

	if _, err := tag.api.UpdateTag(context.Background(), tag.project.ID, id, client.TagNameRequest{Name: "Checkout"}); err != nil {
		t.Fatalf("renaming tag outside Terraform: %s", err)
	}
	tag.create(t, "Payments")
	saved := tag.read(t, imported)

	if got := tagAttribute(t, saved, "name"); got != "Checkout" {
		t.Fatalf("expected original tag's remote name Checkout, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got != id {
		t.Fatalf("expected read to follow imported ID %q, got %q", id, got)
	}
}

func TestAccTagResource_detectsAndReconcilesNameDrift(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	id := tagAttribute(t, created, "id")
	if _, err := tag.api.UpdateTag(context.Background(), tag.project.ID, id, client.TagNameRequest{Name: "Checkout"}); err != nil {
		t.Fatalf("renaming tag outside Terraform: %s", err)
	}

	drifted := tag.read(t, created)
	if got := tagAttribute(t, drifted, "name"); got != "Checkout" {
		t.Fatalf("expected refreshed state to detect remote name Checkout, got %q", got)
	}

	updated := tag.update(t, drifted, "Payments")
	saved := tag.read(t, updated)
	if got := tagAttribute(t, saved, "name"); got != "Payments" {
		t.Fatalf("expected reconciliation to restore Payments, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got != id {
		t.Fatalf("expected reconciliation to preserve ID %q, got %q", id, got)
	}
}

func TestAccTagResource_deleteAlreadyMissingSucceeds(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	if err := tag.api.DeleteTag(context.Background(), tag.project.ID, tagAttribute(t, created, "id")); err != nil {
		t.Fatalf("deleting tag outside Terraform: %s", err)
	}
	var response resource.DeleteResponse

	tag.Delete(context.Background(), resource.DeleteRequest{State: created}, &response)

	requireTagDiagnostics(t, response.Diagnostics)
}

func TestAccTagResource_recreatesMissingTag(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	id := tagAttribute(t, created, "id")
	if err := tag.api.DeleteTag(context.Background(), tag.project.ID, id); err != nil {
		t.Fatalf("deleting tag outside Terraform: %s", err)
	}

	missing := tag.read(t, created)
	if !missing.Raw.IsNull() {
		t.Fatal("externally deleted tag must be removed from state")
	}

	recreated := tag.create(t, "Payments")
	saved := tag.read(t, recreated)
	if got := tagAttribute(t, saved, "name"); got != "Payments" {
		t.Fatalf("expected recreated tag Payments, got %q", got)
	}
	if got := tagAttribute(t, saved, "id"); got == id {
		t.Fatal("recreated tag must have a new identity")
	}
}

func TestAccTagResource_createPreservesDeclaredWhitespace(t *testing.T) {
	tag := newTagTestResource(t)

	created := tag.create(t, "  Payments / EU  ")
	saved := tag.read(t, created)

	if got := tagAttribute(t, created, "name"); got != "  Payments / EU  " {
		t.Fatalf("expected create to preserve declared whitespace, got %q", got)
	}
	if got := tagAttribute(t, saved, "name"); got != "  Payments / EU  " {
		t.Fatalf("expected refresh to avoid whitespace-only drift, got %q", got)
	}

	imported := tag.importTag(t, tag.project.ID+"/Payments / EU")
	if got := tagAttribute(t, imported, "name"); got != "Payments / EU" {
		t.Fatalf("expected API to persist the trimmed name, got %q", got)
	}
	if got := tagAttribute(t, imported, "id"); got != tagAttribute(t, created, "id") {
		t.Fatalf("expected the trimmed name to resolve the created tag, got %q", got)
	}
}

func TestAccTagResource_duplicateCreateSuggestsImport(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")
	plan := tag.plan(t, "payments", types.StringUnknown())
	response := resource.CreateResponse{State: tfsdk.State{Schema: tag.schema, Raw: plan.Raw}}

	tag.Create(context.Background(), resource.CreateRequest{Plan: plan}, &response)

	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Detail(), "already exists") || !strings.Contains(response.Diagnostics[0].Detail(), "import") {
		t.Fatalf("expected a duplicate-name diagnostic suggesting import, got %s", response.Diagnostics)
	}
	saved := tag.read(t, created)
	if got := tagAttribute(t, saved, "name"); got != "Payments" {
		t.Fatalf("duplicate creation must preserve existing tag Payments, got %q", got)
	}
}

func TestAccTagResource_duplicateRenamePreservesBothTags(t *testing.T) {
	tag := newTagTestResource(t)
	payments := tag.create(t, "Payments")
	checkout := tag.create(t, "Checkout")
	plan := tag.plan(t, "payments", types.StringValue(tagAttribute(t, checkout, "id")))
	response := resource.UpdateResponse{State: tfsdk.State{Schema: tag.schema, Raw: plan.Raw}}

	tag.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: checkout}, &response)

	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Detail(), "already exists") || !strings.Contains(response.Diagnostics[0].Detail(), "unused name") {
		t.Fatalf("expected a duplicate rename diagnostic, got %s", response.Diagnostics)
	}
	savedCheckout := tag.read(t, checkout)
	if got := tagAttribute(t, savedCheckout, "name"); got != "Checkout" {
		t.Fatalf("failed rename must preserve Checkout, got %q", got)
	}
	savedPayments := tag.read(t, payments)
	if got := tagAttribute(t, savedPayments, "name"); got != "Payments" {
		t.Fatalf("failed rename must preserve Payments, got %q", got)
	}
}

func TestAccTagResource_updatePreservesDeclaredWhitespace(t *testing.T) {
	tag := newTagTestResource(t)
	created := tag.create(t, "Payments")

	updated := tag.update(t, created, "  Checkout  ")
	saved := tag.read(t, updated)

	if got := tagAttribute(t, updated, "name"); got != "  Checkout  " {
		t.Fatalf("expected update to preserve declared whitespace, got %q", got)
	}
	if got := tagAttribute(t, saved, "name"); got != "  Checkout  " {
		t.Fatalf("expected refresh to avoid whitespace-only drift after update, got %q", got)
	}
	imported := tag.importTag(t, tag.project.ID+"/Checkout")
	if got := tagAttribute(t, imported, "name"); got != "Checkout" {
		t.Fatalf("expected persisted trimmed name Checkout, got %q", got)
	}
	if got := tagAttribute(t, imported, "id"); got != tagAttribute(t, created, "id") {
		t.Fatalf("expected whitespace rename to preserve the created tag ID, got %q", got)
	}
}

func TestAccTagResource_importMissingNameSuggestsCatalogCheck(t *testing.T) {
	tag := newTagTestResource(t)
	response := resource.ImportStateResponse{State: tfsdk.State{
		Schema: tag.schema,
		Raw:    tftypes.NewValue(tag.schema.Type().TerraformType(context.Background()), nil),
	}}

	tag.Resource.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: tag.project.ID + "/Missing"}, &response)

	if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Detail(), "catalog") {
		t.Fatalf("expected missing-tag import diagnostic, got %s", response.Diagnostics)
	}
	if !response.State.Raw.IsNull() {
		t.Fatal("failed import must not adopt a missing tag")
	}
}

func TestTagResource_rejectsNameTooLong(t *testing.T) {
	diagnostics := validateTagNameForTest(t, types.StringValue(strings.Repeat("a", 51)))

	if !diagnostics.HasError() {
		t.Fatal("a 51-character name must be rejected")
	}
}

func TestTagResource_matchesAPILengthForUnicode(t *testing.T) {
	diagnostics := validateTagNameForTest(t, types.StringValue(strings.Repeat("😀", 26)))

	if !diagnostics.HasError() {
		t.Fatal("26 supplementary Unicode characters exceed the API's 50 UTF-16 code unit limit")
	}
}

func TestTagResource_acceptsMaximumLengthAfterTrimming(t *testing.T) {
	diagnostics := validateTagNameForTest(t, types.StringValue("  "+strings.Repeat("a", 50)+"  "))

	requireTagDiagnostics(t, diagnostics)
}

func TestTagResource_defersUnknownNameValidation(t *testing.T) {
	diagnostics := validateTagNameForTest(t, types.StringUnknown())

	requireTagDiagnostics(t, diagnostics)
}

func tagImportDiagnosticsForTest(t *testing.T, identifier string) diag.Diagnostics {
	t.Helper()

	tag := tagResourceForTest(t)
	var definition resource.SchemaResponse
	tag.Schema(context.Background(), resource.SchemaRequest{}, &definition)
	response := resource.ImportStateResponse{State: tfsdk.State{
		Schema: definition.Schema,
		Raw:    tftypes.NewValue(definition.Schema.Type().TerraformType(context.Background()), nil),
	}}

	tag.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: identifier}, &response)
	return response.Diagnostics
}

func TestTagResource_rejectsImportWithoutProject(t *testing.T) {
	diagnostics := tagImportDiagnosticsForTest(t, "/Payments")

	if !diagnostics.HasError() || diagnostics[0].Summary() != "Unexpected Import Identifier" {
		t.Fatalf("expected malformed import diagnostic, got %s", diagnostics)
	}
}

func TestTagResource_rejectsImportWithoutName(t *testing.T) {
	diagnostics := tagImportDiagnosticsForTest(t, "project-example/ \t ")

	if !diagnostics.HasError() || diagnostics[0].Summary() != "Unexpected Import Identifier" {
		t.Fatalf("expected malformed import diagnostic, got %s", diagnostics)
	}
}

func TestTagResource_projectChangeRequiresReplacement(t *testing.T) {
	tag := tagResourceForTest(t)
	var definition resource.SchemaResponse
	tag.Schema(context.Background(), resource.SchemaRequest{}, &definition)
	state := tfsdk.State{Schema: definition.Schema}
	requireTagDiagnostics(t, state.Set(context.Background(), tagTestValues{
		ID: types.StringValue("tag-id"), ProjectID: types.StringValue("old-project"), Name: types.StringValue("Payments"),
	}))
	plan := tfsdk.Plan{Schema: definition.Schema}
	requireTagDiagnostics(t, plan.Set(context.Background(), tagTestValues{
		ID: types.StringValue("tag-id"), ProjectID: types.StringValue("new-project"), Name: types.StringValue("Payments"),
	}))
	request := planmodifier.StringRequest{
		Path: path.Root("project_id"), State: state, Plan: plan,
		StateValue: types.StringValue("old-project"), PlanValue: types.StringValue("new-project"),
	}
	response := planmodifier.StringResponse{PlanValue: request.PlanValue}

	for _, modifier := range definition.Schema.Attributes["project_id"].(schema.StringAttribute).PlanModifiers {
		modifier.PlanModifyString(context.Background(), request, &response)
	}

	requireTagDiagnostics(t, response.Diagnostics)
	if !response.RequiresReplace {
		t.Fatal("moving a tag to another project must require replacement")
	}
}

func TestAccTagResource_preservesDeclaredAPIWhitespace(t *testing.T) {
	tag := newTagTestResource(t)

	created := tag.create(t, "\ufeffPayments\ufeff")
	saved := tag.read(t, created)

	if got := tagAttribute(t, created, "name"); got != "\ufeffPayments\ufeff" {
		t.Fatalf("expected create to preserve API-trimmed Unicode whitespace, got %q", got)
	}
	if got := tagAttribute(t, saved, "name"); got != "\ufeffPayments\ufeff" {
		t.Fatalf("expected refresh to preserve API-trimmed Unicode whitespace, got %q", got)
	}
	imported := tag.importTag(t, tag.project.ID+"/Payments")
	if got := tagAttribute(t, imported, "id"); got != tagAttribute(t, created, "id") {
		t.Fatalf("expected trimmed name to resolve the created tag ID, got %q", got)
	}
}

func TestAccTagResource_importByHexadecimalProjectSlug(t *testing.T) {
	fixture := newTestAccProject(t)
	fixture.Slug = strings.ReplaceAll(uuid.NewString(), "-", "")
	tag := newTagTestResourceWithProject(t, fixture)
	created := tag.create(t, "Payments")

	imported := tag.importTag(t, fixture.Slug+"/Payments")
	saved := tag.read(t, imported)

	if got := tagAttribute(t, saved, "project_id"); got != tag.project.ID {
		t.Fatalf("expected hexadecimal slug to resolve owning project ID %q, got %q", tag.project.ID, got)
	}
	if got := tagAttribute(t, saved, "id"); got != tagAttribute(t, created, "id") {
		t.Fatalf("expected hexadecimal slug import to adopt existing tag, got %q", got)
	}
}
