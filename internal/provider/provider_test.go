package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is shared across acceptance tests in this
// package.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"configdirector": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck is run by every acceptance test's PreCheck. These tests
// hit the real ConfigDirector API (there's no fake/mock server), so
// resource.Test's standard TF_ACC gate applies: tests are skipped entirely
// unless TF_ACC is set. Once that gate passes, this just verifies
// CONFIGDIRECTOR_TOKEN is present, e.g.:
//
//	TF_ACC=1 CONFIGDIRECTOR_TOKEN=... go test ./...
//
// CONFIGDIRECTOR_BASE_URL is left untouched: if unset, the provider falls
// back to its own default (the real ConfigDirector API).
func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("CONFIGDIRECTOR_TOKEN") == "" {
		t.Fatal("CONFIGDIRECTOR_TOKEN must be set to run acceptance tests")
	}
}

// testAccBaseURL returns the base URL the provider under test is actually
// using, for tests that need to build their own *client.Client to make
// out-of-band API calls (e.g. deleting a resource behind Terraform's back).
// CONFIGDIRECTOR_BASE_URL is deliberately left unset for normal runs, so
// this mirrors the same fallback the provider itself applies in Configure,
// rather than assuming the env var is always set.
func testAccBaseURL() string {
	if v := os.Getenv("CONFIGDIRECTOR_BASE_URL"); v != "" {
		return v
	}
	return defaultBaseUrl
}
