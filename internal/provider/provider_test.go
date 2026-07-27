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

// testAccConfigure points the provider under test at a backend, controlled
// by the standard TF_ACC environment variable:
//
//   - TF_ACC unset (the default): tests run hermetically against an
//     in-memory fake of the ConfigDirector API, so `go test` works without
//     credentials or network access.
//   - TF_ACC set: tests run against a live ConfigDirector API.
//     CONFIGDIRECTOR_TOKEN must already be set in the environment.
//     CONFIGDIRECTOR_BASE_URL is left untouched: if unset, the provider
//     falls back to its own default (the real ConfigDirector API), e.g.:
//     TF_ACC=1 CONFIGDIRECTOR_TOKEN=... go test ./...
//
// Tests still run via resource.UnitTest either way, since TF_ACC here only
// selects the backend, not whether the test executes.
func testAccConfigure(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") != "" {
		if os.Getenv("CONFIGDIRECTOR_TOKEN") == "" {
			t.Fatal("TF_ACC is set: CONFIGDIRECTOR_TOKEN must also be set to run against a live API")
		}
		return
	}

	srv := newFakeConfigDirectorServer(t)
	t.Setenv("CONFIGDIRECTOR_BASE_URL", srv.URL)
	t.Setenv("CONFIGDIRECTOR_TOKEN", "test-token")
}

// testAccBaseURL returns the base URL the provider under test is actually
// using, for tests that need to build their own *client.Client to make
// out-of-band API calls (e.g. deleting a resource behind Terraform's back).
// CONFIGDIRECTOR_BASE_URL is deliberately left unset for live runs (see
// testAccConfigure), so this mirrors the same fallback the provider itself
// applies in Configure, rather than assuming the env var is always set.
func testAccBaseURL() string {
	if v := os.Getenv("CONFIGDIRECTOR_BASE_URL"); v != "" {
		return v
	}
	return defaultBaseUrl
}
