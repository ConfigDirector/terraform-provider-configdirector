package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccMetricResource_createsAndImportsARetentionMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = {
    event_name = "app_opened"
    filter     = [{ field = "/platform", target_type = "text", operator = "is one of", target_values = ["ios"] }]
    first_day  = 7
    last_day   = 13
  }
  direction = "higher-is-better"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.#", "1"),
					resource.TestCheckResourceAttr("configdirector_metric.test", "kinds.0", "retention"),
					resource.TestCheckNoResourceAttr("configdirector_metric.test", "event_name"),
					checkSavedMetric("configdirector_metric.test", client.Metric{
						Retention: &client.MetricRetention{
							EventName: "app_opened",
							Filter: []client.MetricFilterCondition{
								{Field: "/platform", TargetType: "text", Operator: "is one of", TargetValues: []string{"ios"}},
							},
							FirstDay: 7,
							LastDay:  13,
						},
						Direction: "higher-is-better",
						Kinds:     []string{"retention"},
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

func TestAccMetricResource_retentionDaysChangeReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  direction = "higher-is-better"
`),
			},
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 14, last_day = 20 }
  direction = "higher-is-better"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
					},
				},
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Retention: &client.MetricRetention{EventName: "app_opened", FirstDay: 14, LastDay: 20},
					Direction: "higher-is-better",
					Kinds:     []string{"retention"},
				}),
			},
		},
	})
}

func TestAccMetricResource_aRetentionFirstDayBelowOneFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 0, last_day = 6 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+first\s+day\s+has\s+to\s+be\s+a\s+whole\s+number\s+from\s+1\s+to\s+90\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionLastDayAbove90FailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 60, last_day = 91 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+last\s+day\s+has\s+to\s+be\s+a\s+whole\s+number\s+from\s+1\s+to\s+90\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionFirstDayAbove90FailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 91, last_day = 90 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+first\s+day\s+has\s+to\s+be\s+a\s+whole\s+number\s+from\s+1\s+to\s+90\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionLastDayBelowOneFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 1, last_day = 0 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+last\s+day\s+has\s+to\s+be\s+a\s+whole\s+number\s+from\s+1\s+to\s+90\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionLastDayBeforeTheFirstDayFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 13, last_day = 7 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+last\s+day\s+cannot\s+come\s+before\s+the\s+first\s+day\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionOfOneDayPlansTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 1, last_day = 1 }
  direction = "higher-is-better"
`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccMetricResource_aRetentionFilterFieldThatIsNotAJSONPointerFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = {
    event_name = "app_opened"
    filter     = [{ field = "platform", target_type = "text", operator = "is one of", target_values = ["ios"] }]
    first_day  = 7
    last_day   = 13
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+detail\s+field\s+must\s+be\s+a\s+JSON\s+pointer\s+\(RFC\s+6901\),\s+such\s+as\s+/plan`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionLastDayKnownOnlyAtApplyCreatesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "terraform_data" "last_day" {
  input = 13
}

resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  retention = {
    event_name = "app_opened"
    first_day  = 7
    last_day   = terraform_data.last_day.output
  }
  direction = "higher-is-better"
}
`,
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Retention: &client.MetricRetention{EventName: "app_opened", FirstDay: 7, LastDay: 13},
					Direction: "higher-is-better",
					Kinds:     []string{"retention"},
				}),
			},
		},
	})
}

func TestAccMetricResource_importOfARetentionByItsEventNameFailsSayingToImportItById(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  direction = "higher-is-better"
`),
			},
			{
				ResourceName:  "configdirector_metric.test",
				ImportState:   true,
				ImportStateId: p.Slug + "/app_opened",
				ExpectError:   regexp.MustCompile(`has\s+no\s+metric\s+for\s+the\s+event\s+name\s+"app_opened"\.\s+The\s+event\s+name\s+is\s+matched\s+exactly,\s+letter\s+case\s+included\.\s+A\s+ratio,\s+funnel\s+or\s+retention\s+metric\s+is\s+imported\s+by\s+its\s+id\.`),
			},
		},
	})
}

func TestAccMetricResource_aRetentionKnownOnlyAtApplyCreatesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "terraform_data" "retention" {
  input = { event_name = "app_opened", first_day = 7, last_day = 13 }
}

resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  retention  = terraform_data.retention.output
  direction  = "higher-is-better"
}
`,
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					Retention: &client.MetricRetention{EventName: "app_opened", FirstDay: 7, LastDay: 13},
					Direction: "higher-is-better",
					Kinds:     []string{"retention"},
				}),
			},
		},
	})
}
