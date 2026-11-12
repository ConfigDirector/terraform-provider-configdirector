package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func metricTestConfig(p testAccProject, eventName, body string) string {
	return p.config("test") + fmt.Sprintf(`
resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  event_name = %q
%s
}
`, eventName, body)
}

func savedMetricAt(s *terraform.State, address string) (*client.Metric, error) {
	resourceState, found := s.RootModule().Resources[address]
	if !found {
		return nil, fmt.Errorf("%s not found in state", address)
	}
	api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
	return api.GetMetric(context.Background(), resourceState.Primary.Attributes["project_id"], resourceState.Primary.ID)
}

func filterAsCompared(filter []client.MetricFilterCondition) []string {
	conditions := make([]string, len(filter))
	for i, condition := range filter {
		targetValues := slices.Sorted(slices.Values(condition.TargetValues))
		conditions[i] = fmt.Sprintf("%s %s %s %q", condition.Field, condition.TargetType, condition.Operator, targetValues)
	}
	slices.Sort(conditions)
	return conditions
}

func sameFilter(left, right []client.MetricFilterCondition) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return slices.Equal(filterAsCompared(left), filterAsCompared(right))
}

func checkSavedMetric(address string, expected client.Metric) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		metric, err := savedMetricAt(s, address)
		if err != nil {
			return err
		}
		if metric.EventName != expected.EventName {
			return fmt.Errorf("expected the metric's event name to be %q, the API has %q", expected.EventName, metric.EventName)
		}
		if !sameFilter(metric.Filter, expected.Filter) {
			return fmt.Errorf("expected the metric's filter to be %+v, the API has %+v", expected.Filter, metric.Filter)
		}
		if !equalOptionalText(metric.ValueField, expected.ValueField) {
			return fmt.Errorf("expected the metric's value field to be %s, the API has %s", optionalText(expected.ValueField), optionalText(metric.ValueField))
		}
		if metric.Direction != expected.Direction {
			return fmt.Errorf("expected the metric's direction to be %q, the API has %q", expected.Direction, metric.Direction)
		}
		if !slices.Equal(metric.Kinds, expected.Kinds) {
			return fmt.Errorf("expected the metric to report %v, the API has %v", expected.Kinds, metric.Kinds)
		}
		if !equalOptionalText(metric.Description, expected.Description) {
			return fmt.Errorf("expected the metric's description to be %s, the API has %s", optionalText(expected.Description), optionalText(metric.Description))
		}
		return nil
	}
}

func equalOptionalText(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func optionalText(text *string) string {
	if text == nil {
		return "none"
	}
	return fmt.Sprintf("%q", *text)
}

func stringPointer(text string) *string {
	return &text
}

func TestAccMetricResource_createAndImport(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  description = "A customer paid for their order."
  direction   = "higher-is-better"
  kinds       = ["value-per-context", "conversion"]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("configdirector_metric.test", "project_id", "configdirector_project.test", "id"),
					resource.TestCheckResourceAttrSet("configdirector_metric.test", "id"),
					resource.TestCheckResourceAttr("configdirector_metric.test", "event_name", "checkout_completed"),
					checkSavedMetric("configdirector_metric.test", client.Metric{
						EventName:   "checkout_completed",
						Description: stringPointer("A customer paid for their order."),
						Direction:   "higher-is-better",
						Kinds:       []string{"conversion", "value-per-context"},
					}),
				),
			},
			{
				ResourceName:      "configdirector_metric.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     p.Slug + "/checkout_completed",
			},
		},
	})
}

func TestAccMetricResource_updatesInPlace(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  description = "A customer paid for their order."
  direction   = "higher-is-better"
  kinds       = ["conversion"]
`),
			},
			{
				Config: metricTestConfig(p, "checkout_completed", `
  description = "A customer paid, refunds excluded."
  direction   = "lower-is-better"
  kinds       = ["count-per-context", "value-per-context"]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					EventName:   "checkout_completed",
					Description: stringPointer("A customer paid, refunds excluded."),
					Direction:   "lower-is-better",
					Kinds:       []string{"count-per-context", "value-per-context"},
				}),
			},
		},
	})
}

func TestAccMetricResource_descriptionLeftOutIsRemoved(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  description = "A customer paid for their order."
  direction   = "higher-is-better"
  kinds       = ["conversion"]
`),
			},
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					EventName:   "checkout_completed",
					Description: nil,
					Direction:   "higher-is-better",
					Kinds:       []string{"conversion"},
				}),
			},
		},
	})
}

func TestAccMetricResource_eventNameChangeReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	var projectID, replacedMetricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "id", func(value string) error {
						replacedMetricID = value
						return nil
					}),
				),
			},
			{
				Config: metricTestConfig(p, "order_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkSavedMetric("configdirector_metric.test", client.Metric{
						EventName: "order_completed",
						Direction: "higher-is-better",
						Kinds:     []string{"conversion"},
					}),
					func(s *terraform.State) error {
						api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
						_, err := api.GetMetric(context.Background(), projectID, replacedMetricID)
						var apiError *client.APIError
						if !errors.As(err, &apiError) || apiError.StatusCode != 404 {
							return fmt.Errorf("expected the metric for checkout_completed to be deleted, reading it returned %v", err)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccMetricResource_deletedOutOfBand(t *testing.T) {
	p := newTestAccProject(t)

	var projectID, metricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "id", func(value string) error {
						metricID = value
						return nil
					}),
				),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := api.DeleteMetric(context.Background(), projectID, metricID); err != nil {
						t.Fatalf("deleting metric out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

func TestAccMetricResource_importsAnEventNameContainingASlash(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "Checkout / Completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
			},
			{
				ResourceName:      "configdirector_metric.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     p.Slug + "/Checkout / Completed",
			},
		},
	})
}

func TestAccMetricResource_importOfAnEventNameWithoutAMetricFails(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
			},
			{
				ResourceName:  "configdirector_metric.test",
				ImportState:   true,
				ImportStateId: p.Slug + "/Checkout_Completed",
				ExpectError:   regexp.MustCompile(`has\s+no\s+metric\s+for\s+the\s+event\s+name\s+"Checkout_Completed"`),
			},
		},
	})
}

const checkoutCompletedMetric = `
resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  event_name = "checkout_completed"
  direction  = "higher-is-better"
  kinds      = ["conversion"]
}
`

func experimentWithAnOpenRunConfig(p testAccProject, metricDeclaration, metricsAttribute string) string {
	return p.config("test") + metricDeclaration + fmt.Sprintf(`
resource "configdirector_config" "test" {
  project_id = configdirector_project.test.id
  key        = "checkout-cta-copy"
  role       = "experiment"
  type       = "string"
  variations = [
    { name = "Control", value = "Buy now" },
    { name = "Urgent", value = "Buy now, few left" },
  ]
  initial_value = "Buy now"
%s
}

resource "configdirector_config_targeting_rules" "test" {
  project_id       = configdirector_project.test.id
  config_key       = configdirector_config.test.key
  environment_slug = "test"
  default_value    = "Buy now"
  rules = [
    {
      id     = provider::configdirector::rule_id("checkout-cta-copy-rollout")
      type   = "percentage"
      order  = 0
      target = "percentage"
      percentages = [
        { id = provider::configdirector::rule_id("checkout-cta-copy-rollout-control"), percentage = 50, value = "Buy now" },
        { id = provider::configdirector::rule_id("checkout-cta-copy-rollout-urgent"), percentage = 50, value = "Buy now, few left" },
      ]
    }
  ]
}
`, metricsAttribute)
}

const primaryCheckoutCompletedMetric = `
  metrics = [
    { metric_id = configdirector_metric.test.id, role = "primary", primary_kind = "conversion" },
  ]
`

func checkMetricUsedByAnOpenRun(s *terraform.State) error {
	metric, err := savedMetricAt(s, "configdirector_metric.test")
	if err != nil {
		return err
	}
	if len(metric.Usages) != 1 || metric.Usages[0].ConfigKey != "checkout-cta-copy" || len(metric.Usages[0].OpenRuns) != 1 {
		return fmt.Errorf("expected the metric to be used by checkout-cta-copy with one open run, the API reports usages %+v", metric.Usages)
	}
	return nil
}

func TestAccMetricResource_destroyFailsWhileAnExperimentItIsAttachedToHasAnOpenRun(t *testing.T) {
	p := newTestAccProject(t)

	var metricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentWithAnOpenRunConfig(p, checkoutCompletedMetric, primaryCheckoutCompletedMetric),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkMetricUsedByAnOpenRun,
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "id", func(value string) error {
						metricID = value
						return nil
					}),
				),
			},
			{
				Config:      experimentWithAnOpenRunConfig(p, "", ""),
				ExpectError: regexp.MustCompile(`The\s+metric\s+is\s+attached\s+to\s+1\s+experiment\s+with\s+an\s+open\s+run`),
			},
			{
				Config: experimentWithAnOpenRunConfig(p, checkoutCompletedMetric, `
  metrics = []
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_metric.test", "id", func(value string) error {
						if value != metricID {
							return fmt.Errorf("expected the metric refused a destroy to be kept as %s, the state has %s", metricID, value)
						}
						return nil
					}),
					checkSavedMetric("configdirector_metric.test", client.Metric{
						EventName: "checkout_completed",
						Direction: "higher-is-better",
						Kinds:     []string{"conversion"},
					}),
					checkSavedConfigMetrics(),
				),
			},
		},
	})
}

func TestAccMetricResource_eventNameChangeFailsWhileAnExperimentItIsAttachedToHasAnOpenRun(t *testing.T) {
	p := newTestAccProject(t)

	orderCompletedMetric := `
resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  event_name = "order_completed"
  direction  = "higher-is-better"
  kinds      = ["conversion"]
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentWithAnOpenRunConfig(p, checkoutCompletedMetric, primaryCheckoutCompletedMetric),
				Check:  checkMetricUsedByAnOpenRun,
			},
			{
				Config:      experimentWithAnOpenRunConfig(p, orderCompletedMetric, primaryCheckoutCompletedMetric),
				ExpectError: regexp.MustCompile(`The\s+metric\s+is\s+attached\s+to\s+1\s+experiment\s+with\s+an\s+open\s+run`),
			},
			{
				Config: experimentWithAnOpenRunConfig(p, checkoutCompletedMetric, primaryCheckoutCompletedMetric),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionNoop),
					},
				},
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.test", role: "primary", primaryKind: stringPointer("conversion")},
				),
			},
		},
	})
}

const twoCheckoutCompletedMetrics = `
resource "configdirector_metric" "team" {
  project_id = configdirector_project.test.id
  event_name = "checkout_completed"
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
}

resource "configdirector_metric" "items" {
  project_id  = configdirector_project.test.id
  event_name  = "checkout_completed"
  value_field = "/items"
  direction   = "higher-is-better"
  kinds       = ["value-per-context"]
}
`

func TestAccMetricResource_importsByIdAMetricWhoseEventNameHasTwoMetrics(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + twoCheckoutCompletedMetrics,
			},
			{
				ResourceName: "configdirector_metric.team",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return p.Slug + "/" + s.RootModule().Resources["configdirector_metric.team"].Primary.ID, nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccMetricResource_importOfAnIdWithoutAMetricFails(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
			},
			{
				ResourceName:  "configdirector_metric.test",
				ImportState:   true,
				ImportStateId: p.Slug + "/0b9d6c1e-58a4-4c39-9a7e-3f1c2d5e8b01",
				ExpectError:   regexp.MustCompile(`has\s+no\s+metric\s+with\s+the\s+id\s+"0b9d6c1e-58a4-4c39-9a7e-3f1c2d5e8b01"`),
			},
		},
	})
}

func TestAccMetricResource_importOfAnEventNameWithTwoMetricsFailsListingTheirIds(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("real API tests require TF_ACC=1")
	}
	testAccPreCheck(t)
	p := newTestAccProject(t)
	token := os.Getenv("CONFIGDIRECTOR_TOKEN")
	api := client.New(testAccBaseURL(), token)
	project, err := api.CreateProject(context.Background(), client.CreateProjectRequest{Name: p.Name, Slug: p.Slug})
	if err != nil {
		t.Fatalf("creating the test project: %s", err)
	}
	t.Cleanup(func() {
		if err := api.DeleteProject(context.Background(), project.ID); err != nil {
			t.Errorf("deleting the test project: %s", err)
		}
	})
	filteredMetric, err := api.CreateMetric(context.Background(), project.ID, client.CreateMetricRequest{
		EventName: "checkout_completed",
		Filter: []client.MetricFilterCondition{
			{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team", "enterprise"}},
			{Field: "/items", TargetType: "number", Operator: ">=", TargetValues: []string{"2"}},
			{Field: "/gift", TargetType: "boolean", Operator: "equals", TargetValues: []string{"true"}},
		},
		Direction: "higher-is-better",
		Kinds:     []string{"conversion"},
	})
	if err != nil {
		t.Fatalf("creating the filtered metric: %s", err)
	}
	itemsMetric, err := api.CreateMetric(context.Background(), project.ID, client.CreateMetricRequest{
		EventName:  "checkout_completed",
		ValueField: stringPointer("/items"),
		Direction:  "higher-is-better",
		Kinds:      []string{"value-per-context"},
	})
	if err != nil {
		t.Fatalf("creating the metric with the value field /items: %s", err)
	}

	protocol := newResourceProtocol(t, "configdirector_metric", testAccBaseURL(), token)
	imported, err := protocol.server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{
		TypeName: "configdirector_metric",
		ID:       p.Slug + "/checkout_completed",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(imported.ImportedResources) != 0 {
		t.Fatalf("expected no imported metric, got %d", len(imported.ImportedResources))
	}
	if len(imported.Diagnostics) != 1 || imported.Diagnostics[0].Severity != tfprotov6.DiagnosticSeverityError {
		t.Fatalf("expected one error, got %+v", imported.Diagnostics)
	}
	detailLines := strings.Split(imported.Diagnostics[0].Detail, "\n")
	filteredMetricLine := "  " + filteredMetric.ID + "  checkout_completed · /plan is one of [team, enterprise] and /items >= 2 and /gift is true"
	if !slices.Contains(detailLines, filteredMetricLine) {
		t.Errorf("expected the error to list %q, got %q", filteredMetricLine, imported.Diagnostics[0].Detail)
	}
	itemsMetricLine := "  " + itemsMetric.ID + "  checkout_completed · value from /items"
	if !slices.Contains(detailLines, itemsMetricLine) {
		t.Errorf("expected the error to list %q, got %q", itemsMetricLine, imported.Diagnostics[0].Detail)
	}
}
