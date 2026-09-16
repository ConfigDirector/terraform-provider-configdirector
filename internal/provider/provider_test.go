package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

// testAccProtoV6ProviderFactories is shared across acceptance tests in this
// package.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"configdirector": providerserver.NewProtocol6WithError(New("test")()),
}

const testAccProjectSlugPrefix = "tf-acc-"

var legacyTestProjectSlugs = []string{"test-project", "renamed-project", "project-a", "project-b"}

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") != "" && os.Getenv("CONFIGDIRECTOR_TOKEN") != "" {
		deleteLeftoverTestProjects()
	}
	os.Exit(m.Run())
}

func deleteLeftoverTestProjects() {
	ctx := context.Background()
	c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))

	projects, err := c.ListProjects(ctx)
	if err != nil {
		log.Printf("listing projects to find leftover test projects: %s", err)
		return
	}

	for _, p := range projects {
		if !isTestProjectSlug(p.Slug) {
			continue
		}
		if err := c.DeleteProject(ctx, p.ID); err != nil {
			log.Printf("deleting leftover test project %q (%s): %s", p.Slug, p.ID, err)
			continue
		}
		log.Printf("deleted leftover test project %q (%s)", p.Slug, p.ID)
	}
}

func isTestProjectSlug(slug string) bool {
	return strings.HasPrefix(slug, testAccProjectSlugPrefix) || slices.Contains(legacyTestProjectSlugs, slug)
}

type testAccProject struct {
	Name string
	Slug string
}

func newTestAccProject(t *testing.T) testAccProject {
	t.Helper()

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating test project suffix: %s", err)
	}
	suffix := hex.EncodeToString(b)

	return testAccProject{
		Name: "TF Acc " + suffix,
		Slug: testAccProjectSlugPrefix + suffix,
	}
}

func (p testAccProject) config(label string) string {
	return fmt.Sprintf(`
resource "configdirector_project" %q {
  name = %q
  slug = %q
}
`, label, p.Name, p.Slug)
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
