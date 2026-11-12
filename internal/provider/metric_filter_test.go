package provider

import (
	"context"
	"errors"
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

func TestAccMetricResource_createsAMetricWithAFilterAndAValueField(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team", "enterprise"] },
    { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
  ]
  value_field = "/items"
  direction   = "higher-is-better"
  kinds       = ["conversion", "value-per-context"]
`),
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					EventName: "checkout_completed",
					Filter: []client.MetricFilterCondition{
						{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team", "enterprise"}},
						{Field: "/items", TargetType: "number", Operator: ">=", TargetValues: []string{"2"}},
					},
					ValueField: stringPointer("/items"),
					Direction:  "higher-is-better",
					Kinds:      []string{"conversion", "value-per-context"},
				}),
			},
		},
	})
}

func TestAccMetricResource_appliesASecondMetricOnTheSameEventNameWithAnotherFilter(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "configdirector_metric" "team" {
  project_id = configdirector_project.test.id
  event_name = "checkout_completed"
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
}

resource "configdirector_metric" "gift" {
  project_id = configdirector_project.test.id
  event_name = "checkout_completed"
  filter = [
    { field = "/gift", target_type = "boolean", operator = "equals", target_values = ["true"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkSavedMetric("configdirector_metric.team", client.Metric{
						EventName: "checkout_completed",
						Filter: []client.MetricFilterCondition{
							{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team"}},
						},
						Direction: "higher-is-better",
						Kinds:     []string{"conversion"},
					}),
					checkSavedMetric("configdirector_metric.gift", client.Metric{
						EventName: "checkout_completed",
						Filter: []client.MetricFilterCondition{
							{Field: "/gift", TargetType: "boolean", Operator: "equals", TargetValues: []string{"true"}},
						},
						Direction: "higher-is-better",
						Kinds:     []string{"conversion"},
					}),
				),
			},
		},
	})
}

func TestAccMetricResource_filterChangeReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	var projectID, replacedMetricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] },
  ]
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
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team", "enterprise"] },
  ]
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
						EventName: "checkout_completed",
						Filter: []client.MetricFilterCondition{
							{Field: "/plan", TargetType: "text", Operator: "is one of", TargetValues: []string{"team", "enterprise"}},
						},
						Direction: "higher-is-better",
						Kinds:     []string{"conversion"},
					}),
					func(s *terraform.State) error {
						api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
						_, err := api.GetMetric(context.Background(), projectID, replacedMetricID)
						var apiError *client.APIError
						if !errors.As(err, &apiError) || apiError.StatusCode != 404 {
							return fmt.Errorf("expected the metric filtered on /plan is one of [team] to be deleted, reading it returned %v", err)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccMetricResource_valueFieldChangeReplacesTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	var projectID, replacedMetricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  value_field = "/items"
  direction   = "higher-is-better"
  kinds       = ["value-per-context"]
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
				Config: metricTestConfig(p, "checkout_completed", `
  value_field = "/order/total"
  direction   = "higher-is-better"
  kinds       = ["value-per-context"]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkSavedMetric("configdirector_metric.test", client.Metric{
						EventName:  "checkout_completed",
						ValueField: stringPointer("/order/total"),
						Direction:  "higher-is-better",
						Kinds:      []string{"value-per-context"},
					}),
					func(s *terraform.State) error {
						api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
						_, err := api.GetMetric(context.Background(), projectID, replacedMetricID)
						var apiError *client.APIError
						if !errors.As(err, &apiError) || apiError.StatusCode != 404 {
							return fmt.Errorf("expected the metric with the value field /items to be deleted, reading it returned %v", err)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccMetricResource_moreThanFiveConditionsFailThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] },
    { field = "/country", target_type = "text", operator = "is NOT one of", target_values = ["US"] },
    { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
    { field = "/total", target_type = "number", operator = "<", target_values = ["500"] },
    { field = "/gift", target_type = "boolean", operator = "equals", target_values = ["false"] },
    { field = "/coupon", target_type = "text", operator = "is NOT one of", target_values = ["STAFF"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`filter\s+set\s+must\s+contain\s+at\s+most\s+5\s+elements,\s+got:\s+6`),
			},
		},
	})
}

func TestAccMetricResource_anOperatorOfAnotherTargetTypeFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/items", target_type = "number", operator = "is one of", target_values = ["2"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+operator\s+of\s+a\s+number\s+condition\s+must\s+be\s+one\s+of:\s+=,\s+!=,\s+>,\s+>=,\s+<,\s+<=`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldWithoutValuePerContextFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  value_field = "/items"
  direction   = "higher-is-better"
  kinds       = ["conversion", "count-per-context"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+metric\s+with\s+a\s+value\s+field\s+needs\s+value-per-context\s+among\s+its\s+kinds`),
			},
		},
	})
}

func TestAccMetricResource_aFilterWithoutConditionsFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter    = []
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+filter\s+needs\s+at\s+least\s+one\s+condition\.\s+Leave\s+filter\s+out\s+for\s+a\s+metric\s+without\s+a\s+filter\.`),
			},
		},
	})
}

func TestAccMetricResource_anUnknownTargetTypeFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/ordered_at", target_type = "date", operator = "is one of", target_values = ["2026-11-12"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`target_type\s+value\s+must\s+be\s+one\s+of:\s+\["boolean"\s+"number"\s+"text"\],\s+got:\s+"date"`),
			},
		},
	})
}

func TestAccMetricResource_aConditionFieldThatIsNotAJSONPointerFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "plan", target_type = "text", operator = "is one of", target_values = ["team"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+detail\s+field\s+must\s+be\s+a\s+JSON\s+pointer\s+\(RFC\s+6901\),\s+such\s+as\s+/plan`),
			},
		},
	})
}

func TestAccMetricResource_aConditionFieldWithAnInvalidEscapeFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/discount~2code", target_type = "text", operator = "is one of", target_values = ["WELCOME"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+detail\s+field\s+must\s+be\s+a\s+JSON\s+pointer\s+\(RFC\s+6901\),\s+such\s+as\s+/plan`),
			},
		},
	})
}

func TestAccMetricResource_aConditionFieldWithEscapesPlansTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/discount~1coupon/code~0name", target_type = "text", operator = "is one of", target_values = ["WELCOME"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccMetricResource_aConditionFieldLongerThan500CharactersFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", fmt.Sprintf(`
  filter = [
    { field = %q, target_type = "text", operator = "is one of", target_values = ["team"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`, "/"+strings.Repeat("p", 500))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`field\s+UTF-8\s+character\s+count\s+must\s+be\s+at\s+most\s+500,\s+got:\s+501`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldThatIsNotAJSONPointerFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  value_field = "items"
  direction   = "higher-is-better"
  kinds       = ["value-per-context"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+detail\s+field\s+must\s+be\s+a\s+JSON\s+pointer\s+\(RFC\s+6901\),\s+such\s+as\s+/plan`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldLongerThan500CharactersFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", fmt.Sprintf(`
  value_field = %q
  direction   = "higher-is-better"
  kinds       = ["value-per-context"]
`, "/"+strings.Repeat("i", 500))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value_field\s+UTF-8\s+character\s+count\s+must\s+be\s+at\s+most\s+500,\s+got:\s+501`),
			},
		},
	})
}

func TestAccMetricResource_aTextConditionWithoutTargetValuesFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is NOT one of", target_values = [] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+text\s+condition\s+needs\s+at\s+least\s+one\s+target\s+value`),
			},
		},
	})
}

func TestAccMetricResource_aTextConditionWithMoreThan20TargetValuesFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    {
      field         = "/country"
      target_type   = "text"
      operator      = "is one of"
      target_values = ["AR", "AT", "AU", "BE", "BR", "CA", "CH", "CL", "DE", "DK", "ES", "FI", "FR", "GB", "IE", "IT", "JP", "MX", "NL", "NO", "SE"]
    },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+text\s+condition\s+cannot\s+have\s+more\s+than\s+20\s+target\s+values`),
			},
		},
	})
}

func TestAccMetricResource_aNumberConditionWithTwoTargetValuesFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/items", target_type = "number", operator = ">=", target_values = ["2", "3"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+number\s+condition\s+needs\s+exactly\s+one\s+target\s+value`),
			},
		},
	})
}

func TestAccMetricResource_aNumberConditionWithATargetThatIsNotANumberFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/items", target_type = "number", operator = ">=", target_values = ["two"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+target\s+value\s+of\s+a\s+number\s+condition\s+must\s+be\s+a\s+number`),
			},
		},
	})
}

func TestAccMetricResource_aNumberConditionWithADecimalTargetPlansTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/total", target_type = "number", operator = "<", target_values = [-9.99] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccMetricResource_aBooleanConditionWithTwoTargetValuesFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/gift", target_type = "boolean", operator = "equals", target_values = ["true", "false"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+boolean\s+condition\s+needs\s+exactly\s+one\s+target\s+value,\s+true\s+or\s+false`),
			},
		},
	})
}

func TestAccMetricResource_aBooleanConditionWithATargetOtherThanTrueOrFalseFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/gift", target_type = "boolean", operator = "equals", target_values = ["yes"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The\s+target\s+value\s+of\s+a\s+boolean\s+condition\s+must\s+be\s+true\s+or\s+false`),
			},
		},
	})
}

func TestAccMetricResource_aBlankTargetValueFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team", "  "] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+target\s+value\s+cannot\s+be\s+blank`),
			},
		},
	})
}

func TestAccMetricResource_aTargetValueLongerThan500CharactersFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", fmt.Sprintf(`
  filter = [
    { field = "/coupon", target_type = "text", operator = "is one of", target_values = [%q] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`, strings.Repeat("C", 501))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`UTF-8\s+character\s+count\s+must\s+be\s+at\s+most\s+500,\s+got:\s+501`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldKnownOnlyAtApplyPlansTheMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + `
resource "terraform_data" "value_field" {
  input = null
}

resource "configdirector_metric" "test" {
  project_id  = configdirector_project.test.id
  event_name  = "checkout_completed"
  value_field = terraform_data.value_field.output
  direction   = "higher-is-better"
  kinds       = ["conversion"]
}
`,
				Check: checkSavedMetric("configdirector_metric.test", client.Metric{
					EventName: "checkout_completed",
					Direction: "higher-is-better",
					Kinds:     []string{"conversion"},
				}),
			},
		},
	})
}

func TestAccMetricResource_reorderingTheConditionsAndTargetValuesPlansNoChange(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["team", "enterprise"] },
    { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
			},
			{
				Config: metricTestConfig(p, "checkout_completed", `
  filter = [
    { field = "/items", target_type = "number", operator = ">=", target_values = ["2"] },
    { field = "/plan", target_type = "text", operator = "is one of", target_values = ["enterprise", "team"] },
  ]
  direction = "higher-is-better"
  kinds     = ["conversion"]
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
