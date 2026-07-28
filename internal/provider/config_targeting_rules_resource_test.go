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

const targetingRulesTestConfigConfig = configTestProjectConfig + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`

func TestAccConfigTargetingRulesResource_createAndImport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules = [
    {
      id    = provider::configdirector::rule_id("rule-1")
      type  = "conditional"
      order = 0
      conditions = [
        {
          id           = provider::configdirector::rule_id("rule-1-cond-1")
          attribute    = "appName"
          operator     = "equals"
          targetType   = "text"
          targetValues = ["myapp"]
        }
      ]
      target = "value"
      value  = false
    }
  ]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("configdirector_config_targeting_rules.test", "project_id", "configdirector_project.test", "id"),
					resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "config_key", "test-flag-key"),
					resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "environment_slug", "test"),
					resource.TestCheckResourceAttrSet("configdirector_config_targeting_rules.test", "environment_id"),
					resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "default_value", "true"),
				),
			},
			{
				ResourceName: "configdirector_config_targeting_rules.test",
				ImportState:  true,
				// environment_id, not "id" (this resource has no "id" attribute
				// it's identified by project_id/config_key/environment).
				ImportStateVerifyIdentifierAttribute: "environment_id",
				ImportStateVerify:                    true,
				// rules is write-only (the API embeds extra generated fields
				// into whatever's written, so it's never reconciled against a
				// read - see the schema description), so it can't be populated
				// by import.
				ImportStateVerifyIgnore: []string{"rules"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_config_targeting_rules.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.Attributes["config_key"], rs.Primary.Attributes["environment_slug"]), nil
				},
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_importByProjectSlug covers using the
// project's slug (not just its id) as the first segment of the import ID -
// mirroring configdirector_project's own ImportState, which resolves a
// slug via the project list since there's no get-by-slug endpoint.
func TestAccConfigTargetingRulesResource_importByProjectSlug(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules            = []
}
`,
			},
			{
				ResourceName:                         "configdirector_config_targeting_rules.test",
				ImportState:                          true,
				ImportStateVerifyIdentifierAttribute: "environment_id",
				ImportStateVerify:                    true,
				ImportStateVerifyIgnore:              []string{"rules"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_config_targeting_rules.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s/%s", "test-project", rs.Primary.Attributes["config_key"], rs.Primary.Attributes["environment_slug"]), nil
				},
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_planAfterImport checks for the same
// class of bug fixed in configdirector_config's initial_value (see
// TestAccConfigResource_planAfterImport): default_value/rules aren't fully
// populated by ImportState (rules is write-only and never reconciled at
// all - see the schema description), so a plan immediately after import
// could conflict with a configured value the same way initial_value did,
// if either attribute pinned the plan to a possibly-null state value. They
// don't (no custom PlanModifiers on either), so this is expected to pass
// without any provider changes - it's here to prove that, not to drive a
// fix.
//
// The project/config/target are set up directly via the client (not a
// prior apply step) for the same reason as the config_resource_test.go
// version: ImportStatePersist's first step needs a clean import into empty
// state.
func TestAccConfigTargetingRulesResource_planAfterImport(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	testAccPreCheck(t)

	ctx := context.Background()
	c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))

	project, err := c.CreateProject(ctx, client.CreateProjectRequest{Name: "Test Project", Slug: "test-project"})
	if err != nil {
		t.Fatalf("creating project: %s", err)
	}
	t.Cleanup(func() {
		if err := c.DeleteProject(ctx, project.ID); err != nil {
			t.Logf("cleaning up project %s: %s", project.ID, err)
		}
	})

	if _, err := c.CreateConfig(ctx, project.ID, client.CreateConfigRequest{
		Key:          "test-flag-key",
		Role:         "flag",
		Lifetime:     "temporary",
		Type:         "boolean",
		DefaultValue: true,
	}); err != nil {
		t.Fatalf("creating config: %s", err)
	}

	envs, err := c.ListEnvironments(ctx, project.ID)
	if err != nil {
		t.Fatalf("listing environments: %s", err)
	}
	var envID string
	for _, e := range envs {
		if e.Slug == "test" {
			envID = e.ID
		}
	}
	if envID == "" {
		t.Fatal("no \"test\" environment found")
	}

	if err := c.UpdateConfigTargets(ctx, project.ID, "test-flag-key", client.UpdateConfigTargetsRequest{
		EnvironmentID: envID,
		DefaultValue:  "false",
		Rules:         []any{},
	}); err != nil {
		t.Fatalf("setting targeting rules: %s", err)
	}

	config := fmt.Sprintf(`
resource "configdirector_config_targeting_rules" "test" {
  project_id       = %q
  config_key       = "test-flag-key"
  environment_slug = "test"
  default_value    = "false"
  rules            = []
}
`, project.ID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "configdirector_config_targeting_rules.test",
				ImportState:        true,
				ImportStatePersist: true,
				ImportStateId:      project.ID + "/test-flag-key/test",
			},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "default_value", "false"),
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_updatesInPlace verifies that
// default_value/rules changes update the targeting rules in place via the
// API, not a replace. The framework's automatic post-apply refresh+plan
// confirms default_value was actually persisted through the API (rules
// itself is write-only and never reconciled, so it can't be verified the
// same way - see the schema description).
func TestAccConfigTargetingRulesResource_updatesInPlace(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules            = []
}
`,
			},
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "false"
  rules            = []
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config_targeting_rules.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "default_value", "false"),
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_environmentChangeForcesReplacement
// verifies the RequiresReplace plan modifier on environment_slug: this
// resource manages one config's targeting state for one specific
// environment, so switching environments is a different resource, not an
// update to this one.
func TestAccConfigTargetingRulesResource_environmentChangeForcesReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules            = []
}
`,
			},
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "production"
  default_value    = "true"
  rules            = []
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config_targeting_rules.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr("configdirector_config_targeting_rules.test", "environment_slug", "production"),
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_deletedOutOfBand verifies Read()'s
// 404 handling: there's no dedicated GET for a single target, so Read works
// by fetching the parent config and looking up the matching targets entry
// (see targetingRulesToModel) - deleting the parent config out of band
// should drop this resource from state too.
func TestAccConfigTargetingRulesResource_deletedOutOfBand(t *testing.T) {
	var projectID, configKey string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules            = []
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_config_targeting_rules.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_config_targeting_rules.test", "config_key", func(value string) error {
						configKey = value
						return nil
					}),
				),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := c.DeleteConfig(context.Background(), projectID, configKey); err != nil {
						t.Fatalf("deleting config out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("configdirector_config_targeting_rules.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// TestAccConfigTargetingRulesResource_duplicateRuleIds covers the
// rulesUniqueIDs validator: every rule/condition/percentage-bucket id must
// be unique across the whole rules value. This fails during plan, before
// any API call is made.
func TestAccConfigTargetingRulesResource_duplicateRuleIds(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: targetingRulesTestConfigConfig + `
resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "true"
  rules = [
    {
      id    = provider::configdirector::rule_id("dup")
      type  = "conditional"
      order = 0
      conditions = [
        {
          id           = provider::configdirector::rule_id("dup")
          attribute    = "appName"
          operator     = "equals"
          targetType   = "text"
          targetValues = ["myapp"]
        }
      ]
      target = "value"
      value  = false
    }
  ]
}
`,
				ExpectError: regexp.MustCompile(`Duplicate rule/condition/percentage-bucket id`),
			},
		},
	})
}
