package provider

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccMetricResource_turningAMetricOfOneEventNameIntoAFunnelReplacesIt(t *testing.T) {
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
				Config: metricDeclaredAs(p, `
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = "checkout_completed" },
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

func TestAccMetricResource_twoDefinitionsFailThePlan(t *testing.T) {
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
      { event_name = "checkout_completed" },
    ]
  }
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+metric\s+is\s+a\s+ratio,\s+a\s+funnel\s+or\s+a\s+retention\s+metric,\s+never\s+two\s+of\s+them\.\s+Set\s+one\s+of\s+the\s+three\.`),
			},
		},
	})
}

func TestAccMetricResource_anEventNameBesideARatioFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  ratio = {
    numerator   = { event_name = "checkout_completed", adds = "value" }
    denominator = { event_name = "checkout_completed", adds = "events" }
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+ratio\s+metric\s+has\s+no\s+event\s+name\s+of\s+its\s+own:\s+its\s+numerator\s+and\s+its\s+denominator\s+each\s+take\s+one\.\s+Leave\s+event_name\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aFilterBesideAFunnelFailsThePlan(t *testing.T) {
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
      { event_name = "checkout_completed" },
    ]
  }
  filter    = [{ field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] }]
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+metric\s+has\s+no\s+filter\s+of\s+its\s+own:\s+each\s+of\s+its\s+steps\s+takes\s+one\.\s+Leave\s+filter\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldBesideARetentionFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention   = { event_name = "app_opened", first_day = 7, last_day = 13 }
  value_field = "/minutes"
  direction   = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+retention\s+metric\s+has\s+no\s+value\s+field:\s+it\s+counts\s+the\s+contexts\s+that\s+reported\s+the\s+event\s+during\s+their\s+days\.\s+Leave\s+value_field\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_kindsBesideARatioFailThePlan(t *testing.T) {
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
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+ratio\s+metric\s+reports\s+the\s+ratio\s+kind\s+alone,\s+so\s+kinds\s+does\s+not\s+apply\s+to\s+it\.\s+Leave\s+kinds\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aMetricWithoutAnEventNameOrADefinitionFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+metric\s+needs\s+an\s+event_name,\s+a\s+ratio,\s+a\s+funnel\s+or\s+a\s+retention\.`),
			},
		},
	})
}

func TestAccMetricResource_anEventNameWithoutKindsFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+metric\s+with\s+an\s+event_name\s+needs\s+kinds,\s+at\s+least\s+one\s+of:\s+conversion,\s+count-per-context,\s+value-per-context\.`),
			},
		},
	})
}

func TestMetricResource_theRatioKindBesideARatioFailsWithTheRatioRefusalAlone(t *testing.T) {
	protocol := newResourceProtocol(t, "configdirector_metric", "https://api.example.com", "metric-test-token")
	ratioType := protocol.typeOf.AttributeTypes["ratio"].(tftypes.Object)
	sideType := ratioType.AttributeTypes["numerator"].(tftypes.Object)
	side := func(adds string) tftypes.Value {
		return tftypes.NewValue(sideType, map[string]tftypes.Value{
			"event_name":  tftypes.NewValue(tftypes.String, "checkout_completed"),
			"adds":        tftypes.NewValue(tftypes.String, adds),
			"filter":      tftypes.NewValue(sideType.AttributeTypes["filter"], nil),
			"value_field": tftypes.NewValue(tftypes.String, nil),
		})
	}
	configuration := protocol.config(t, map[string]tftypes.Value{
		"project_id": tftypes.NewValue(tftypes.String, "8c3f2a1e-5b4d-4e6f-9a7b-1c2d3e4f5a6b"),
		"ratio":      tftypes.NewValue(ratioType, map[string]tftypes.Value{"numerator": side("value"), "denominator": side("events")}),
		"direction":  tftypes.NewValue(tftypes.String, "higher-is-better"),
		"kinds":      tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "ratio")}),
	})

	validated, err := protocol.server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "configdirector_metric",
		Config:   configuration,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(validated.Diagnostics) != 1 {
		t.Fatalf("expected one error, got %d: %+v", len(validated.Diagnostics), validated.Diagnostics)
	}
	expectedDetail := "A ratio metric reports the ratio kind alone, so kinds does not apply to it. Leave kinds out."
	if validated.Diagnostics[0].Detail != expectedDetail {
		t.Errorf("expected the error %q, got %q", expectedDetail, validated.Diagnostics[0].Detail)
	}
}

func TestAccMetricResource_anEventNameBesideAFunnelFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  funnel = {
    steps = [
      { event_name = "cart_viewed" },
      { event_name = "checkout_completed" },
    ]
  }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+metric\s+has\s+no\s+event\s+name\s+of\s+its\s+own:\s+each\s+of\s+its\s+steps\s+takes\s+one\.\s+Leave\s+event_name\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_anEventNameBesideARetentionFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "app_opened", `
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+retention\s+metric\s+has\s+no\s+event\s+name\s+of\s+its\s+own:\s+its\s+retention\s+takes\s+one\.\s+Leave\s+event_name\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aFilterBesideARatioFailsThePlan(t *testing.T) {
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
  filter    = [{ field = "/plan", target_type = "text", operator = "is one of", target_values = ["team"] }]
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+ratio\s+metric\s+has\s+no\s+filter\s+of\s+its\s+own:\s+its\s+numerator\s+and\s+its\s+denominator\s+each\s+take\s+one\.\s+Leave\s+filter\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aFilterBesideARetentionFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  filter    = [{ field = "/platform", target_type = "text", operator = "is one of", target_values = ["ios"] }]
  direction = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+retention\s+metric\s+has\s+no\s+filter\s+of\s+its\s+own:\s+its\s+retention\s+takes\s+one\.\s+Leave\s+filter\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldBesideARatioFailsThePlan(t *testing.T) {
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
  value_field = "/order/total"
  direction   = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+ratio\s+metric\s+has\s+no\s+value\s+field\s+of\s+its\s+own:\s+its\s+numerator\s+and\s+its\s+denominator\s+each\s+take\s+one\.\s+Leave\s+value_field\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_aValueFieldBesideAFunnelFailsThePlan(t *testing.T) {
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
      { event_name = "checkout_completed" },
    ]
  }
  value_field = "/order/total"
  direction   = "higher-is-better"
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+metric\s+has\s+no\s+value\s+field:\s+it\s+counts\s+the\s+contexts\s+that\s+reached\s+each\s+step\.\s+Leave\s+value_field\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_kindsBesideAFunnelFailThePlan(t *testing.T) {
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
      { event_name = "checkout_completed" },
    ]
  }
  direction = "higher-is-better"
  kinds     = ["funnel"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+funnel\s+metric\s+reports\s+the\s+funnel\s+kind\s+alone,\s+so\s+kinds\s+does\s+not\s+apply\s+to\s+it\.\s+Leave\s+kinds\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_kindsBesideARetentionFailThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricDeclaredAs(p, `
  retention = { event_name = "app_opened", first_day = 7, last_day = 13 }
  direction = "higher-is-better"
  kinds     = ["conversion"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`A\s+retention\s+metric\s+reports\s+the\s+retention\s+kind\s+alone,\s+so\s+kinds\s+does\s+not\s+apply\s+to\s+it\.\s+Leave\s+kinds\s+out\.`),
			},
		},
	})
}

func TestAccMetricResource_theRatioKindOnAMetricWithAnEventNameFailsThePlan(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: metricTestConfig(p, "checkout_completed", `
  direction = "higher-is-better"
  kinds     = ["ratio"]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value\s+must\s+be\s+one\s+of:\s+\["conversion"\s+"count-per-context"\s+"value-per-context"\],\s+got:\s+"ratio"`),
			},
		},
	})
}
