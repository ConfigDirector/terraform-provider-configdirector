package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const environmentTestProjectConfig = `
resource "configdirector_project" "test" {
  name = "Test Project"
  slug = "test-project"
}
`

func TestAccEnvironmentResource_createAndImport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("configdirector_environment.test", "id"),
					resource.TestCheckResourceAttrPair("configdirector_environment.test", "project_id", "configdirector_project.test", "id"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "name", "Staging"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "slug", "staging-env"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "color", "blue"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "live", "false"),
				),
			},
			{
				ResourceName:      "configdirector_environment.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_environment.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.Attributes["id"]), nil
				},
			},
		},
	})
}

// TestAccEnvironmentResource_importBySlug covers ImportState's alternate
// lookup path: project_id/slug, not just project_id/id (see
// environment_resource.go's ImportState).
func TestAccEnvironmentResource_importBySlug(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
			},
			{
				ResourceName:      "configdirector_environment.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_environment.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.Attributes["slug"]), nil
				},
			},
		},
	})
}

// TestAccEnvironmentResource_updatesInPlace verifies that name/color/live
// changes update the environment in place via the API. project_id is the
// only attribute that still forces a replace, since the API has no way to
// move an environment between projects. The id staying constant confirms
// it's a true update; the framework's automatic post-apply refresh+plan
// confirms the change was actually persisted through the API.
func TestAccEnvironmentResource_updatesInPlace(t *testing.T) {
	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
				Check: resource.TestCheckResourceAttrWith("configdirector_environment.test", "id", func(value string) error {
					idBeforeUpdate = value
					return nil
				}),
			},
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging Renamed"
  slug       = "staging-env"
  color      = "red"
  live       = true
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_environment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_environment.test", "name", "Staging Renamed"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "color", "red"),
					resource.TestCheckResourceAttr("configdirector_environment.test", "live", "true"),
					resource.TestCheckResourceAttrWith("configdirector_environment.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after update, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccEnvironmentResource_slugChangeUpdatesInPlace(t *testing.T) {
	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
				Check: resource.TestCheckResourceAttrWith("configdirector_environment.test", "id", func(value string) error {
					idBeforeUpdate = value
					return nil
				}),
			},
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env-renamed"
  color      = "blue"
  live       = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_environment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_environment.test", "slug", "staging-env-renamed"),
					resource.TestCheckResourceAttrWith("configdirector_environment.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after slug change, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

// TestAccEnvironmentResource_deletedOutOfBand verifies Read()'s 404
// handling: when an environment has been deleted outside of Terraform,
// refreshing state should drop it from state, leaving a plan that wants to
// create it again.
func TestAccEnvironmentResource_deletedOutOfBand(t *testing.T) {
	var projectID, environmentID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_environment.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_environment.test", "id", func(value string) error {
						environmentID = value
						return nil
					}),
				),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := c.DeleteEnvironment(context.Background(), projectID, environmentID); err != nil {
						t.Fatalf("deleting environment out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_environment.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// TestAccEnvironmentResource_validation covers the schema-level validators
// on name/slug/color. These fail during plan, before any API call is made.
func TestAccEnvironmentResource_validation(t *testing.T) {
	testCases := map[string]struct {
		config      string
		expectError string
	}{
		"name too long": {
			config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "` + strings.Repeat("a", 201) + `"
  slug       = "staging-env"
  color      = "blue"
  live       = false
}
`,
			expectError: `string length must be between 1 and 200`,
		},
		"slug too short": {
			config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "abc"
  color      = "blue"
  live       = false
}
`,
			expectError: `string length must be between 4 and 150`,
		},
		"invalid color": {
			config: environmentTestProjectConfig + `
resource "configdirector_environment" "test" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging-env"
  color      = "not-a-color"
  live       = false
}
`,
			expectError: `value must be one of`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      tc.config,
						ExpectError: regexp.MustCompile(tc.expectError),
					},
				},
			})
		})
	}
}
