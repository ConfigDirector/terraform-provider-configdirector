package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccMetricResource_createsAndImportsARatioMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator = {
      event_name  = "checkout_completed"
      adds        = "value"
      filter      = [{ field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] }]
      value_field = "/order/total"
    }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  description = "Revenue per order on the Team plan."
  direction   = "higher-is-better"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.#", "1"),
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.0", "ratio"),
					resource.TestCheckNoResourceAttr("configdirector_metric.test", "event_name"),
					checkSavedMetric("configdirector_metric.test", client.Metric{
						Ratio: &client.MetricRatio{
							Numerator: client.MetricRatioSide{
								EventName: "checkout_completed",
								Adds:      "value",
								Filter: []client.MetricFilterCondition{
									{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team"}},
								},
								ValueField: stringPointer("/order/total"),
							},
							Denominator: client.MetricRatioSide{EventName: "checkout_completed", Adds: "events"},
						},
						Description: stringPointer("Revenue per order on the Team plan."),
						Direction:   "higher-is-better",
						Kinds:       []string{"ratio"},
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

func TestAccMetricResource_ratioChangeReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "value" }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
			},
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "value" }
    denominator = { event_name = "session_started", adds = "events" }
  }
  direction = "higher-is-better"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
					},
				},
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Ratio: &client.MetricRatio{
						Numerator:   client.MetricRatioSide{EventName: "checkout_completed", Adds: "value"},
						Denominator: client.MetricRatioSide{EventName: "session_started", Adds: "events"},
					},
					Direction: "higher-is-better",
					Kinds:     []string{"ratio"},
				}),
			},
		},
	})
}

func TestAccMetricResource_reorderingTheConditionsOfARatioSidePlansNoChange(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator = {
      event_name = "checkout_completed"
      adds       = "events"
      filter = [
        { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team", "enterprise"] },
        { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
      ]
    }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
			},
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator = {
      event_name = "checkout_completed"
      adds       = "events"
      filter = [
        { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
        { field = "/plan", target_type = "text", operator = "is one of", target_values = ["enterprise", "team"] },
      ]
    }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}

func TestAccMetricResource_aRatioSideThatAddsEventsWithAValueFieldFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "value" }
    denominator = { event_name = "checkout_completed", adds = "events", value_field = "/items" }
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+side\s+that\s+adds\s+events\s+cannot\s+have\s+a\s+value\s+field\.\s+Set\s+adds\s+to\s+value\s+to\s+sum\s+the\s+field\.`),
			},
		},
	})
}

func TestAccMetricResource_aRatioSideThatAddsNeitherEventsNorValueFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "contexts" }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value\s+must\s+be\s+one\s+of:\s+\["events"\s+"value"\]`),
			},
		},
	})
}

func TestAccMetricResource_anOperatorOfAnotherTargetTypeOnARatioSideFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator = {
      event_name = "checkout_completed"
      adds       = "events"
      filter     = [{ field = "/items", target_type = "number", operator = "is one of", target_values = ["2"] }]
    }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+operator\s+of\s+a\s+number\s+condition\s+must\s+be\s+one\s+of:\s+=,\s+!=,\s+>,\s+>=,\s+<,\s+<=`),
			},
		},
	})
}

func TestAccMetricResource_aRatioSideValueFieldThatIsNotAJSONPointerFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "value", value_field = "order.total" }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+detail\s+field\s+must\s+be\s+a\s+JSON\s+pointer\s+\(RFC\s+6901\),\s+such\s+as\s+/plan`),
			},
		},
	})
}
