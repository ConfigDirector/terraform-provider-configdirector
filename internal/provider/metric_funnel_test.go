package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccMetricResource_createsAndImportsAFunnelMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = "checkout_started" },
      {
        event_name = "checkout_completed"
        filter     = [{ field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] }]
      },
    ]
  }
  direction = "higher-is-better"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.#", "1"),
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.0", "funnel"),
					resource.TestCheckNoResourceAttr("configdirector_metric.test", "event_name"),
					checkSavedMetric("configdirector_metric.test", client.Metric{
						Funnel: &client.MetricFunnel{Steps: []client.MetricFunnelStep{
							{EventName: "cart_viewed"},
							{EventName: "checkout_started"},
							{
								EventName: "checkout_completed",
								Filter: []client.MetricFilterCondition{
									{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team"}},
								},
							},
						}},
						Direction: "higher-is-better",
						Kinds:     []string{"funnel"},
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName: "configdirector_metric.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return p.Slug + "/" + s.RootModule().Resources["configdirector_metric.test"].Primary.ID, nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccMetricResource_reorderingTheStepsOfAFunnelReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = "checkout_started" },
    ]
  }
  direction = "higher-is-better"
`),
			},
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "checkout_started" },
      { event_name = "cart_viewed" },
    ]
  }
  direction = "higher-is-better"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
					},
				},
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Funnel: &client.MetricFunnel{Steps: []client.MetricFunnelStep{
						{EventName: "checkout_started"},
						{EventName: "cart_viewed"},
					}},
					Direction: "higher-is-better",
					Kinds:     []string{"funnel"},
				}),
			},
		},
	})
}

func TestAccMetricResource_aFunnelOfOneStepFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel    = { steps = [{ event_name = "checkout_completed" }] }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+has\s+2\s+to\s+5\s+steps\.`),
			},
		},
	})
}

func TestAccMetricResource_aFunnelOfSixStepsFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "product_viewed" },
      { event_name = "cart_viewed" },
      { event_name = "checkout_started" },
      { event_name = "shipping_entered" },
      { event_name = "payment_entered" },
      { event_name = "checkout_completed" },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+has\s+2\s+to\s+5\s+steps\.`),
			},
		},
	})
}

func TestAccMetricResource_aFunnelStepThatRepeatsAnEarlierOneFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = "checkout_started" },
      { event_name = "cart_viewed" },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Step\s+3\s+repeats\s+step\s+1:\s+the\s+same\s+event\s+name\s+and\s+filter\.\s+Change\s+one\s+of\s+them\s+or\s+remove\s+the\s+step\.`),
			},
		},
	})
}

func TestAccMetricResource_aFunnelStepWhoseFilterWritesTheSameNumberAnotherWayRepeatsAnEarlierOne(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      {
        event_name = "checkout_completed"
        filter     = [{ field = "/items", target_type = "number", operator = ">=", target_values = ["2"] }]
      },
      {
        event_name = "checkout_completed"
        filter     = [{ field = "/items", target_type = "number", operator = ">=", target_values = ["2.0"] }]
      },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Step\s+2\s+repeats\s+step\s+1:\s+the\s+same\s+event\s+name\s+and\s+filter\.`),
			},
		},
	})
}

func TestAccMetricResource_aFunnelStepWhoseFilterComparesWithMinusZeroRepeatsAStepComparingWithZero(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      {
        event_name = "checkout_completed"
        filter     = [{ field = "/discount", target_type = "number", operator = "=", target_values = ["0"] }]
      },
      {
        event_name = "checkout_completed"
        filter     = [{ field = "/discount", target_type = "number", operator = "=", target_values = ["-0"] }]
      },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Step\s+2\s+repeats\s+step\s+1:\s+the\s+same\s+event\s+name\s+and\s+filter\.`),
			},
		},
	})
}

func TestAccMetricResource_twoFunnelStepsOfOneEventNameWithDifferentFiltersPlanTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      {
        event_name = "page_viewed"
        filter     = [{ field = "/page", target_type = "text", operator = "is one of", target_values = ["pricing"] }]
      },
      {
        event_name = "page_viewed"
        filter     = [{ field = "/page", target_type = "text", operator = "is one of", target_values = ["signup"] }]
      },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccMetricResource_aFunnelStepFilterWithoutConditionsFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "cart_viewed", filter = [] },
      { event_name = "checkout_completed" },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+filter\s+needs\s+at\s+least\s+one\s+condition\.`),
			},
		},
	})
}

func TestAccMetricResource_aFunnelStepEventNameLongerThan150CharactersFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, fmt.Sprintf(`
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = %q },
    ]
  }
  direction = "higher-is-better"
`, strings.Repeat("e", 151))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`UTF-8\s+character\s+count\s+must\s+be\s+between\s+1\s+and\s+150,\s+got:\s+151`),
			},
		},
	})
}

func TestAccMetricResource_funnelStepEventNamesKnownOnlyAtApplyCreateTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "terraform_data" "step_event_names" {
  input = ["cart_viewed", "checkout_completed"]
}

resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  funnel = {
    steps = [
      { event_name = terraform_data.step_event_names.output[0] },
      { event_name = terraform_data.step_event_names.output[1] },
    ]
  }
  direction = "higher-is-better"
}
`,
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Funnel: &client.MetricFunnel{Steps: []client.MetricFunnelStep{
						{EventName: "cart_viewed"},
						{EventName: "checkout_completed"},
					}},
					Direction: "higher-is-better",
					Kinds:     []string{"funnel"},
				}),
			},
		},
	})
}
