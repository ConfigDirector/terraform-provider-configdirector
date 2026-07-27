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

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
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
