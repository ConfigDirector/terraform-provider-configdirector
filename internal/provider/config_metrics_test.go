package provider

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

const checkoutMetrics = `
resource "configdirector_metric" "checkout" {
  project_id = configdirector_project.test.id
  event_name = "checkout_completed"
  direction  = "higher-is-better"
  kinds      = ["conversion", "value-per-context"]
}

resource "configdirector_metric" "failures" {
  project_id = configdirector_project.test.id
  event_name = "checkout_failed"
  direction  = "lower-is-better"
  kinds      = ["count-per-context"]
}
`

func experimentTestConfig(p testAccProject, body string) string {
	return p.config("test") + checkoutMetrics + fmt.Sprintf(`
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
`, body)
}

type expectedConfigMetric struct {
	metricAddress string
	role          string
	primaryKind   *string
}

func savedConfigFromState(s *terraform.State) (*client.Config, error) {
	resourceState, found := s.RootModule().Resources["configdirector_config.test"]
	if !found {
		return nil, fmt.Errorf("configdirector_config.test not found in state")
	}
	api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
	return api.GetConfig(context.Background(), resourceState.Primary.Attributes["project_id"], resourceState.Primary.Attributes["key"])
}

func metricAttachmentsInRoleOrder(attachments []client.MetricAttachment) []client.MetricAttachment {
	return slices.SortedFunc(slices.Values(attachments), func(left, right client.MetricAttachment) int {
		return cmp.Or(cmp.Compare(left.Role, right.Role), cmp.Compare(left.MetricID, right.MetricID))
	})
}

func describeMetricAttachments(attachments []client.MetricAttachment) string {
	described := make([]string, len(attachments))
	for i, attachment := range attachments {
		described[i] = fmt.Sprintf("{%s %s %s}", attachment.MetricID, attachment.Role, optionalText(attachment.PrimaryKind))
	}
	return fmt.Sprint(described)
}

func checkSavedConfigMetrics(expected ...expectedConfigMetric) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		expectedAttachments := make([]client.MetricAttachment, len(expected))
		for i, metric := range expected {
			resourceState, found := s.RootModule().Resources[metric.metricAddress]
			if !found {
				return fmt.Errorf("%s not found in state", metric.metricAddress)
			}
			expectedAttachments[i] = client.MetricAttachment{MetricID: resourceState.Primary.ID, Role: metric.role, PrimaryKind: metric.primaryKind}
		}

		config, err := savedConfigFromState(s)
		if err != nil {
			return err
		}
		saved := metricAttachmentsInRoleOrder(config.Metrics)
		wanted := metricAttachmentsInRoleOrder(expectedAttachments)
		if !slices.EqualFunc(saved, wanted, func(left, right client.MetricAttachment) bool {
			return left.MetricID == right.MetricID && left.Role == right.Role && equalOptionalText(left.PrimaryKind, right.PrimaryKind)
		}) {
			return fmt.Errorf("expected the experiment's metrics to be %s, the API has %s", describeMetricAttachments(wanted), describeMetricAttachments(saved))
		}
		return nil
	}
}

func checkSavedHypothesis(expected *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		config, err := savedConfigFromState(s)
		if err != nil {
			return err
		}
		if !equalOptionalText(config.Hypothesis, expected) {
			return fmt.Errorf("expected the experiment's hypothesis to be %s, the API has %s", optionalText(expected), optionalText(config.Hypothesis))
		}
		return nil
	}
}

func TestAccConfigMetrics_experimentAttachesMetricsAndAHypothesis(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  hypothesis = "Urgent copy gets more customers to check out."
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
    { metric_id = configdirector_metric.failures.id, role = "guardrail" },
  ]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkSavedConfigMetrics(
						expectedConfigMetric{metricAddress: "configdirector_metric.checkout", role: "primary", primaryKind: stringPointer("conversion")},
						expectedConfigMetric{metricAddress: "configdirector_metric.failures", role: "guardrail"},
					),
					checkSavedHypothesis(stringPointer("Urgent copy gets more customers to check out.")),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            "configdirector_config.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"initial_value"},
				ImportStateId:           p.Slug + "/checkout-cta-copy",
			},
		},
	})
}

func TestAccConfigMetrics_metricsLeftOutKeepTheAttachedMetrics(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
  ]
`),
			},
			{
				Config: experimentTestConfig(p, `
  description = "Which call to action gets more checkouts."
`),
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.checkout", role: "primary", primaryKind: stringPointer("conversion")},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigMetrics_emptyMetricsDetachEveryMetric(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
    { metric_id = configdirector_metric.failures.id, role = "guardrail" },
  ]
`),
			},
			{
				Config: experimentTestConfig(p, `
  metrics = []
`),
				Check: checkSavedConfigMetrics(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigMetrics_declaredMetricsCorrectMetricsAttachedOutsideTerraform(t *testing.T) {
	p := newTestAccProject(t)

	declared := experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
  ]
`)
	var projectID, failuresMetricID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: declared,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("configdirector_config.test", "project_id", func(value string) error {
						projectID = value
						return nil
					}),
					resource.TestCheckResourceAttrWith("configdirector_metric.failures", "id", func(value string) error {
						failuresMetricID = value
						return nil
					}),
				),
			},
			{
				PreConfig: func() {
					api := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if _, err := api.UpdateConfigMetrics(context.Background(), projectID, "checkout-cta-copy", []client.MetricAttachment{
						{MetricID: failuresMetricID, Role: "guardrail"},
					}); err != nil {
						t.Fatalf("attaching a metric out of band: %s", err)
					}
				},
				Config: declared,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.checkout", role: "primary", primaryKind: stringPointer("conversion")},
				),
			},
		},
	})
}

func TestAccConfigMetrics_hypothesisLeftOutIsKept(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  hypothesis = "Urgent copy gets more customers to check out."
`),
			},
			{
				Config: experimentTestConfig(p, `
  description = "Which call to action gets more checkouts."
`),
				Check: checkSavedHypothesis(stringPointer("Urgent copy gets more customers to check out.")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigMetrics_emptyHypothesisRemovesIt(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  hypothesis = "Urgent copy gets more customers to check out."
`),
			},
			{
				Config: experimentTestConfig(p, `
  hypothesis = ""
`),
				Check: checkSavedHypothesis(nil),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccConfigMetrics_experimentWhoseMetricsAreRefusedOnCreateIsReplacedOnTheNextApply(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.failures.id, role = "primary", primary_kind = "conversion" },
  ]
`),
				ExpectError: regexp.MustCompile(`The\s+metric\s+for\s+checkout_failed\s+does\s+not\s+report\s+conversion`),
			},
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.failures.id, role = "primary", primary_kind = "count-per-context" },
  ]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionReplace),
					},
				},
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.failures", role: "primary", primaryKind: stringPointer("count-per-context")},
				),
			},
		},
	})
}

func TestAccConfigMetrics_rolesChangeInPlace(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
    { metric_id = configdirector_metric.failures.id, role = "guardrail" },
  ]
`),
			},
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "secondary" },
    { metric_id = configdirector_metric.failures.id, role = "primary", primary_kind = "count-per-context" },
  ]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
					PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.checkout", role: "secondary"},
					expectedConfigMetric{metricAddress: "configdirector_metric.failures", role: "primary", primaryKind: stringPointer("count-per-context")},
				),
			},
		},
	})
}

func TestAccConfigMetrics_metricWithoutARoleFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id },
  ]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Each\s+metric\s+needs\s+a\s+role\.`),
			},
		},
	})
}

func TestAccConfigMetrics_metricWithoutAMetricIdFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: experimentTestConfig(p, `
  metrics = [
    { role = "guardrail" },
  ]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Each\s+metric\s+needs\s+a\s+metric_id\.`),
			},
		},
	})
}

func nonExperimentTestConfig(p testAccProject, roleSettings, body string) string {
	return p.config("test") + checkoutMetrics + fmt.Sprintf(`
resource "configdirector_config" "test" {
  project_id    = configdirector_project.test.id
  key           = "new-checkout-flow"
  type          = "boolean"
  initial_value = false
%s
%s
}
`, roleSettings, body)
}

func TestAccConfigMetrics_metricsOnAFlagFailThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nonExperimentTestConfig(p, `role = "flag"`, `
  metrics = [
    { metric_id = configdirector_metric.checkout.id, role = "primary", primary_kind = "conversion" },
  ]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Only\s+an\s+experiment\s+can\s+have\s+metrics\.`),
			},
		},
	})
}

func TestAccConfigMetrics_hypothesisOnAFlagFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nonExperimentTestConfig(p, `role = "flag"`, `
  hypothesis = "The new flow gets more customers to check out."
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Only\s+an\s+experiment\s+can\s+have\s+a\s+hypothesis\.`),
			},
		},
	})
}

func TestAccConfigMetrics_experimentAttachesTheMetricThatReplacesOne(t *testing.T) {
	p := newTestAccProject(t)

	metric := func(eventName string) string {
		return fmt.Sprintf(`
resource "configdirector_metric" "test" {
  project_id = configdirector_project.test.id
  event_name = %q
  direction  = "higher-is-better"
  kinds      = ["conversion"]
}
`, eventName)
	}
	experiment := `
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
  metrics = [
    { metric_id = configdirector_metric.test.id, role = "primary", primary_kind = "conversion" },
  ]
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test") + metric("checkout_completed") + experiment,
			},
			{
				Config: p.config("test") + metric("order_completed") + experiment,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_metric.test", plancheck.ResourceActionReplace),
						plancheck.ExpectResourceAction("configdirector_config.test", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkSavedConfigMetrics(
					expectedConfigMetric{metricAddress: "configdirector_metric.test", role: "primary", primaryKind: stringPointer("conversion")},
				),
			},
		},
	})
}

func TestAccConfigMetrics_metricsOnAKillSwitchFailThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nonExperimentTestConfig(p, `role = "kill-switch"`, `
  metrics = [
    { metric_id = configdirector_metric.failures.id, role = "guardrail" },
  ]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Only\s+an\s+experiment\s+can\s+have\s+metrics\.`),
			},
		},
	})
}

func TestAccConfigMetrics_hypothesisOnAConfigFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nonExperimentTestConfig(p, `
  role     = "config"
  lifetime = "permanent"
`, `
  hypothesis = "Checkouts go up when the new flow is on."
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Only\s+an\s+experiment\s+can\s+have\s+a\s+hypothesis\.`),
			},
		},
	})
}
