package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccConfigResource_createAndImport(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
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

func TestAccConfigResource_killSwitchCreatedWithoutLifetimeIsPermanent(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-kill-switch-key"
  role          = "kill-switch"
  type          = "boolean"
  initial_value = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("lifetime"), knownvalue.StringExact("permanent")),
					},
				},
				Check: resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "permanent"),
			},
		},
	})
}

func TestAccConfigResource_flagBecomesPermanentKillSwitchInPlace(t *testing.T) {
	p := newTestAccProject(t)

	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  type          = "boolean"
  initial_value = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "temporary"),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
						idBeforeUpdate = value
						return nil
					}),
				),
			},
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "kill-switch"
  type          = "boolean"
  initial_value = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("lifetime"), knownvalue.StringExact("permanent")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_config.test", "role", "kill-switch"),
					resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "permanent"),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after the role change, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccConfigResource_killSwitchBecomesTemporaryFlagInPlace(t *testing.T) {
	p := newTestAccProject(t)

	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-kill-switch-key"
  role          = "kill-switch"
  type          = "boolean"
  initial_value = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "permanent"),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
						idBeforeUpdate = value
						return nil
					}),
				),
			},
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-kill-switch-key"
  role          = "flag"
  type          = "boolean"
  initial_value = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("lifetime"), knownvalue.StringExact("temporary")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_config.test", "role", "flag"),
					resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "temporary"),
					resource.TestCheckResourceAttrWith("configdirector_config.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after the role change, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccConfigResource_roleChangeOutsideFlagAndKillSwitchFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "flag"
  type          = "boolean"
  initial_value = false
}
`,
			},
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-flag-key"
  role          = "experiment"
  type          = "boolean"
  initial_value = false
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Only\s+a\s+feature\s+flag\s+can\s+become\s+a\s+kill\s+switch,\s+and\s+a\s+kill\s+switch\s+a\s+feature\s+flag\.`),
			},
		},
	})
}

func TestAccConfigResource_lifetimeContradictingTheRoleFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-kill-switch-key"
  role          = "kill-switch"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A kill switch is always permanent\.`),
			},
		},
	})
}

func TestAccConfigResource_configCreatedWithoutLifetimeFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-config-key"
  role          = "config"
  type          = "string"
  initial_value = "classic"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A config needs a lifetime\.`),
			},
		},
	})
}

func TestAccConfigResource_configKeepsItsLifetimeWhenLeftOut(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-config-key"
  role          = "config"
  lifetime      = "permanent"
  type          = "string"
  initial_value = "classic"
}
`,
			},
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-config-key"
  description   = "Checkout theme"
  role          = "config"
  type          = "string"
  initial_value = "classic"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("lifetime"), knownvalue.StringExact("permanent")),
					},
				},
				Check: resource.TestCheckResourceAttr("configdirector_config.test", "lifetime", "permanent"),
			},
		},
	})
}

func TestAccConfigResource_integerExperimentKeepsNumberVariationValues(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "integer"
  variations    = [{ name = "Off", value = 0 }, { name = "Small", value = 10 }]
  initial_value = 0
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("variations"), knownvalue.TupleExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Off"), "value": knownvalue.Int64Exact(0)}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Small"), "value": knownvalue.Int64Exact(10)}),
					})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigResource_floatExperimentKeepsNumberVariationValues(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "float"
  variations    = [{ name = "Half", value = 0.5 }, { name = "Tiny", value = 0.0000001 }, { name = "Huge", value = 1e21 }]
  initial_value = 0.5
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("variations"), knownvalue.TupleExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Half"), "value": knownvalue.Float64Exact(0.5)}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Tiny"), "value": knownvalue.Float64Exact(0.0000001)}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Huge"), "value": knownvalue.Float64Exact(1e21)}),
					})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigResource_booleanExperimentKeepsBooleanVariationValues(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "boolean"
  variations    = [{ name = "Enabled", value = true }, { name = "Disabled", value = false }]
  initial_value = false
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("variations"), knownvalue.TupleExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Enabled"), "value": knownvalue.Bool(true)}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Disabled"), "value": knownvalue.Bool(false)}),
					})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigResource_updatedIntegerExperimentKeepsNumberVariationValues(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "integer"
  variations    = [{ name = "Off", value = 0 }, { name = "Small", value = 10 }]
  initial_value = 0
}
`,
			},
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "integer"
  variations    = [{ name = "Off", value = 0 }, { name = "Large", value = 20 }]
  initial_value = 0
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("configdirector_config.test", tfjsonpath.New("variations"), knownvalue.TupleExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Off"), "value": knownvalue.Int64Exact(0)}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Large"), "value": knownvalue.Int64Exact(20)}),
					})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate)},
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

type refreshedVariationsCheck struct {
	variations knownvalue.Check
}

func (check refreshedVariationsCheck) CheckPlan(ctx context.Context, request plancheck.CheckPlanRequest, response *plancheck.CheckPlanResponse) {
	for _, change := range request.Plan.ResourceChanges {
		if change.Address != "configdirector_config.test" {
			continue
		}

		variations, err := tfjsonpath.Traverse(change.Change.Before, tfjsonpath.New("variations"))
		if err != nil {
			response.Error = err
			return
		}
		response.Error = check.variations.CheckValue(variations)
		return
	}
	response.Error = errors.New("expected a planned change to configdirector_config.test")
}

func TestAccConfigResource_importedIntegerExperimentStoresVariationValuesAsText(t *testing.T) {
	p := newTestAccProject(t)

	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	testAccPreCheck(t)

	ctx := context.Background()
	c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))

	project, err := c.CreateProject(ctx, client.CreateProjectRequest{Name: p.Name, Slug: p.Slug})
	if err != nil {
		t.Fatalf("creating project: %s", err)
	}
	t.Cleanup(func() {
		if err := c.DeleteProject(ctx, project.ID); err != nil {
			t.Logf("cleaning up project %s: %s", project.ID, err)
		}
	})

	off, small := "Off", "Small"
	if _, err := c.CreateConfig(ctx, project.ID, client.CreateConfigRequest{
		Key:          "test-experiment-key",
		Role:         "experiment",
		Type:         "integer",
		Variations:   []client.Variation{{Name: &off, Value: 0}, {Name: &small, Value: 10}},
		DefaultValue: 0,
	}); err != nil {
		t.Fatalf("creating config: %s", err)
	}

	config := fmt.Sprintf(`
resource "configdirector_config" "test" {
  project_id    = %q
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "integer"
  variations    = [{ name = "Off", value = 0 }, { name = "Small", value = 10 }]
  initial_value = 0
}
`, project.ID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "configdirector_config.test",
				ImportState:        true,
				ImportStatePersist: true,
				ImportStateId:      project.ID + "/test-experiment-key",
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						refreshedVariationsCheck{knownvalue.TupleExact([]knownvalue.Check{
							knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Off"), "value": knownvalue.StringExact("0")}),
							knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Small"), "value": knownvalue.StringExact("10")}),
						})},
					},
				},
			},
		},
	})
}

func TestAccConfigResource_variationValueChangedOutsideTerraformShowsAsDrift(t *testing.T) {
	p := newTestAccProject(t)

	var projectID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "test-experiment-key"
  role          = "experiment"
  type          = "integer"
  variations    = [{ name = "Off", value = 0 }, { name = "Small", value = 10 }]
  initial_value = 0
}
`,
				Check: resource.TestCheckResourceAttrWith("configdirector_config.test", "project_id", func(value string) error {
					projectID = value
					return nil
				}),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					off, large := "Off", "Large"
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if _, err := c.UpdateConfig(context.Background(), projectID, "test-experiment-key", client.UpdateConfigRequest{
						Key:        "test-experiment-key",
						Role:       "experiment",
						Type:       "integer",
						Variations: []client.Variation{{Name: &off, Value: 0}, {Name: &large, Value: 30}},
						Server:     true,
						Client:     true,
					}); err != nil {
						t.Fatalf("changing a variation value out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						refreshedVariationsCheck{knownvalue.TupleExact([]knownvalue.Check{
							knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Off"), "value": knownvalue.Int64Exact(0)}),
							knownvalue.ObjectExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("Large"), "value": knownvalue.StringExact("30")}),
						})},
					},
				},
			},
		},
	})
}

// TestAccConfigResource_planAfterImport reproduces a "Provider produced
// invalid plan" crash: import leaves initial_value null in state (the API
// never returns a config's default value back), but ignoreUpdatesAfterCreate
// unconditionally pins the plan to whatever's in state once state exists -
// including that null - which conflicts with a non-null configured value on
// this Required attribute.
//
// The project/config are created directly via the client (not a prior
// apply step) so that ImportStatePersist's first step is a clean import
// into empty state: importing into an address Terraform already manages
// fails outright ("Resource already managed by Terraform"), and the
// state-stripping that would otherwise allow re-importing over prior state
// only exists for the newer plannable (import block) mode, which forbids
// ImportStatePersist entirely.
func TestAccConfigResource_planAfterImport(t *testing.T) {
	p := newTestAccProject(t)

	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	testAccPreCheck(t)

	ctx := context.Background()
	c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))

	project, err := c.CreateProject(ctx, client.CreateProjectRequest{Name: p.Name, Slug: p.Slug})
	if err != nil {
		t.Fatalf("creating project: %s", err)
	}
	t.Cleanup(func() {
		if err := c.DeleteProject(ctx, project.ID); err != nil {
			t.Logf("cleaning up project %s: %s", project.ID, err)
		}
	})

	defaultValue := true
	if _, err := c.CreateConfig(ctx, project.ID, client.CreateConfigRequest{
		Key:          "test-flag-key",
		Role:         "flag",
		Lifetime:     "temporary",
		Type:         "boolean",
		DefaultValue: defaultValue,
	}); err != nil {
		t.Fatalf("creating config: %s", err)
	}

	config := fmt.Sprintf(`
resource "configdirector_config" "test" {
  project_id    = %q
  key           = "test-flag-key"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = true
}
`, project.ID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "configdirector_config.test",
				ImportState:        true,
				ImportStatePersist: true,
				ImportStateId:      project.ID + "/test-flag-key",
			},
			{
				// The fixed behavior: initial_value was null in state after
				// import (nothing to pin to), so the plan modifier should let
				// the configured value flow through and adopt it as the new
				// baseline - not crash, and not require a second apply.
				Config: config,
				Check:  resource.TestCheckResourceAttr("configdirector_config.test", "initial_value", "true"),
			},
		},
	})
}

// TestAccConfigResource_importByProjectSlug covers using the project's
// slug (not just its id) as the first segment of the import ID - mirroring
// configdirector_project's own ImportState, which resolves a slug via the
// project list since there's no get-by-slug endpoint.
func TestAccConfigResource_importByProjectSlug(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
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
				ResourceName:            "configdirector_config.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"initial_value"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_config.test"]
					if !ok {
						return "", nil
					}
					return fmt.Sprintf("%s/%s", p.Slug, rs.Primary.Attributes["key"]), nil
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
	p := newTestAccProject(t)

	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
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
				Config: p.config("test") + `
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
	a := newTestAccProject(t)
	b := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: a.config("a") + b.config("b") + `
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
				Config: a.config("a") + b.config("b") + `
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
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
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
				Config: p.config("test") + `
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
	p := newTestAccProject(t)

	var projectID, key string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
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
	p := newTestAccProject(t)

	testCases := map[string]struct {
		config      string
		expectError string
	}{
		"key too short": {
			config: p.config("test") + `
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
			config: p.config("test") + `
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
			config: p.config("test") + `
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
			config: p.config("test") + `
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
			config: p.config("test") + `
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
