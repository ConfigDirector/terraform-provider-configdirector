package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const configTestProjectConfig = `
resource "configdirector_project" "test" {
  name = "Test Project"
  slug = "test-project"
}
`

func TestAccConfigResource_createAndImport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("configdirector_config.test", "id"),
					resource.TestCheckResourceAttrPair("configdirector_config.test", "project_id", "configdirector_project.test", "id"),
					resource.TestCheckResourceAttr("configdirector_config.test", "key", "test-flag-key"),
					resource.TestCheckResourceAttr("configdirector_config.test", "role", "flag"),
					resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "temporary"),
					resource.TestCheckResourceAttr("configdirector_config.test", "type", "boolean"),
					resource.TestCheckResourceAttrSet("configdirector_config.test", "state"),
					resource.TestCheckResourceAttr("configdirector_config.test", "client", "true"),
					resource.TestCheckResourceAttr("configdirector_config.test", "server", "true"),
				),
			},
			{
				ResourceName:      "configdirector_config.test",
				ImportState:       true,
				ImportStateVerify: true,
				// initial_value is write-only (the API never returns a config's
				// default value back - see the schema description), so it can't
				// be populated by import.
				ImportStateVerifyIgnore: []string{"initial_value"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_config.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.Attributes["key"]), nil
				},
			},
		},
	})
}

// TestAccConfigResource_updatesInPlace verifies that description/client/
// server changes update the config in place via the API (PATCH), not a
// replace. The id staying constant confirms it's a true update; the
// framework's automatic post-apply refresh+plan confirms the change was
// actually persisted through the API.
func TestAccConfigResource_updatesInPlace(t *testing.T) {
	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  client        = true
  server        = true
  initial_value = true
}
`,
				Check: resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
					idBeforeUpdate = value
					return nil
				}),
			},
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  description   = "Updated description"
  client        = false
  server        = true
  initial_value = true
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_config.test", "description", "Updated description"),
					resource.TestCheckResourceAttr("configdirector_config.test", "client", "false"),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
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

// TestAccConfigResource_projectIdChangeForcesReplacement verifies the
// RequiresReplace plan modifier on project_id: the API has no way to move a
// config between projects.
func TestAccConfigResource_projectIdChangeForcesReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "configdirector_project" "a" {
  name = "Project A"
  slug = "project-a"
}

resource "configdirector_project" "b" {
  name = "Project B"
  slug = "project-b"
}

resource "configdirector_config" "test" {
  project_id    = configdirector_project.a.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
			},
			{
				Config: `
resource "configdirector_project" "a" {
  name = "Project A"
  slug = "project-a"
}

resource "configdirector_project" "b" {
  name = "Project B"
  slug = "project-b"
}

resource "configdirector_config" "test" {
  project_id    = configdirector_project.b.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttrPair("configdirector_config.test", "project_id", "configdirector_project.b", "id"),
			},
		},
	})
}

// TestAccConfigResource_initialValueIgnoredAfterCreate verifies the
// documented behavior of initial_value's ignoreUpdatesAfterCreate plan
// modifier: the API has no endpoint to change a config's default value in
// place, so the value is pinned to whatever was set at create time.
// Changing it afterward keeps showing a pending plan (it's never reconciled
// away) but never reaches the API.
func TestAccConfigResource_initialValueIgnoredAfterCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
			},
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}
`,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccConfigResource_deletedOutOfBand verifies Read()'s 404 handling:
// when a config has been deleted outside of Terraform, refreshing state
// should drop it from state, leaving a plan that wants to create it again.
func TestAccConfigResource_deletedOutOfBand(t *testing.T) {
	var projectID, key string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_config.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "key", func(value string) error {
						key = value
						return nil
					}),
				),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := c.DeleteConfig(context.Background(), projectID, key); err != nil {
						t.Fatalf("deleting config out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// TestAccConfigResource_validation covers the schema-level validators on
// key/role/lifetime/type, plus the custom cross-attribute validator
// requiring role "experiment" for variations. These fail during plan,
// before any API call is made.
func TestAccConfigResource_validation(t *testing.T) {
	testCases := map[string]struct {
		config      string
		expectError string
	}{
		"key too short": {
			config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "abc"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
			expectError: `string length must be between 4 and 150`,
		},
		"invalid role": {
			config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "not-a-role"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`,
			expectError: `value must be one of`,
		},
		"invalid lifetime": {
			config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "not-a-lifetime"
  type          = "boolean"
  initial_value = true
}
`,
			expectError: `value must be one of`,
		},
		"invalid type": {
			config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "not-a-type"
  initial_value = true
}
`,
			expectError: `value must be one of`,
		},
		"variations without experiment role": {
			config: configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
  variations    = [{ value = true }, { value = false }]
}
`,
			expectError: `variations can only be set when role is "experiment"`,
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
