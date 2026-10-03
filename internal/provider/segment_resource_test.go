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

const acmeStaffGroups = `
  groups = [
    [
      {
        id           = provider::configdirector::rule_id("acme-staff-email")
        attribute    = "traits"
        trait        = "/email"
        operator     = "ends with any of"
        targetType   = "text"
        targetValues = ["@acme.com"]
      }
    ]
  ]
`

func segmentTestConfig(p testAccProject, key, name, body string) string {
	return p.config("test") + fmt.Sprintf(`
resource "configdirector_segment" "test" {
  project_id = configdirector_project.test.id
  key        = %q
  name       = %q
%s
}
`, key, name, body)
}

func segmentFromState(s *terraform.State) (*client.Segment, error) {
	rs, ok := s.RootModule().Resources["configdirector_segment.test"]
	if !ok {
		return nil, fmt.Errorf("configdirector_segment.test not found in state")
	}
	c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
	return c.GetSegment(context.Background(), rs.Primary.Attributes["project_id"], rs.Primary.Attributes["key"])
}

func checkSegmentOverrideCount(expected int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		segment, err := segmentFromState(s)
		if err != nil {
			return err
		}
		if len(segment.Overrides) != expected {
			return fmt.Errorf("expected %d override(s) on the segment, the API has %d", expected, len(segment.Overrides))
		}
		return nil
	}
}

func checkSegmentGroupCount(expected int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		segment, err := segmentFromState(s)
		if err != nil {
			return err
		}
		groups, ok := segment.Groups.([]any)
		if !ok {
			return fmt.Errorf("expected the segment's groups to be a list, got %T", segment.Groups)
		}
		if len(groups) != expected {
			return fmt.Errorf("expected %d group(s) on the segment, the API has %d", expected, len(groups))
		}
		return nil
	}
}

func TestAccSegmentResource_createAndImport(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", acmeStaffGroups),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("configdirector_segment.test", "project_id", "configdirector_project.test", "id"),
					resource.TestCheckResourceAttrSet("configdirector_segment.test", "id"),
					resource.TestCheckResourceAttr("configdirector_segment.test", "key", "acme-staff"),
					resource.TestCheckResourceAttr("configdirector_segment.test", "name", "Acme staff"),
					resource.TestCheckResourceAttr("configdirector_segment.test", "kind", "rule-based"),
					checkSegmentGroupCount(1),
					checkSegmentOverrideCount(0),
				),
			},
			{
				ResourceName:            "configdirector_segment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"groups", "overrides"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_segment.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s", p.Slug, rs.Primary.Attributes["key"]), nil
				},
			},
		},
	})
}

func TestAccSegmentResource_updatesInPlace(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", acmeStaffGroups),
			},
			{
				Config: segmentTestConfig(p, "partner-staff", "Partner staff", `
  groups = [
    [
      {
        id           = provider::configdirector::rule_id("acme-staff-email")
        attribute    = "traits"
        trait        = "/email"
        operator     = "ends with any of"
        targetType   = "text"
        targetValues = ["@acme.com"]
      }
    ],
    [
      {
        id           = provider::configdirector::rule_id("partner-staff-email")
        attribute    = "traits"
        trait        = "/email"
        operator     = "ends with any of"
        targetType   = "text"
        targetValues = ["@partner.com"]
      }
    ]
  ]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_segment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_segment.test", "key", "partner-staff"),
					resource.TestCheckResourceAttr("configdirector_segment.test", "name", "Partner staff"),
					checkSegmentGroupCount(2),
				),
			},
		},
	})
}

func TestAccSegmentResource_overridesAreOwnedByTheResource(t *testing.T) {
	p := newTestAccProject(t)

	staging := `
resource "configdirector_environment" "staging" {
  project_id = configdirector_project.test.id
  name       = "Staging"
  slug       = "staging"
  color      = "purple"
  live       = false
}
`
	withOverride := acmeStaffGroups + `
  overrides = {
    (configdirector_environment.staging.id) = [
      [
        {
          id           = provider::configdirector::rule_id("staging-everyone")
          attribute    = "identifier"
          operator     = "is one of"
          targetType   = "text"
          targetValues = ["user-1", "user-2"]
        }
      ]
    ]
  }
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", withOverride) + staging,
				Check:  checkSegmentOverrideCount(1),
			},
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", acmeStaffGroups) + staging,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_segment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkSegmentOverrideCount(0),
			},
		},
	})
}

func TestAccSegmentResource_deletedOutOfBand(t *testing.T) {
	p := newTestAccProject(t)

	var projectID, key string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", acmeStaffGroups),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_segment.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_segment.test", "key", func(value string) error {
						key = value
						return nil
					}),
				),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := c.DeleteSegment(context.Background(), projectID, key); err != nil {
						t.Fatalf("deleting segment out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_segment.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

func TestAccSegmentResource_duplicateConditionIds(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", `
  groups = [
    [
      {
        id           = provider::configdirector::rule_id("dup")
        attribute    = "appName"
        operator     = "equals"
        targetType   = "text"
        targetValues = ["myapp"]
      }
    ],
    [
      {
        id           = provider::configdirector::rule_id("dup")
        attribute    = "appName"
        operator     = "equals"
        targetType   = "text"
        targetValues = ["otherapp"]
      }
    ]
  ]
`),
				ExpectError: regexp.MustCompile(`Duplicate condition id`),
			},
		},
	})
}

func TestAccSegmentResource_usedByASegmentConditionInTargetingRules(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: segmentTestConfig(p, "acme-staff", "Acme staff", acmeStaffGroups) + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}

resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "false"
  rules = [
    {
      id     = provider::configdirector::rule_id("acme-staff-rule")
      type   = "conditional"
      order  = 0
      target = "value"
      value  = true
      conditions = [
        {
          id        = provider::configdirector::rule_id("acme-staff-rule-condition")
          kind      = "segment"
          operator  = "in"
          segmentId = configdirector_segment.test.id
        }
      ]
    }
  ]
}
`,
				Check: func(s *terraform.State) error {
					segment, err := segmentFromState(s)
					if err != nil {
						return err
					}
					if len(segment.Usages) != 1 || segment.Usages[0].ConfigKey != "test-flag-key" {
						return fmt.Errorf("expected the segment to be used by test-flag-key, the API reports usages %+v", segment.Usages)
					}
					return nil
				},
			},
		},
	})
}
