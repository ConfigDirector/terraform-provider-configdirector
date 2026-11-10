terraform {
  required_providers {
    configdirector = {
      source  = "ConfigDirector/configdirector"
      version = "~> 0.8"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Metric Example"
  slug = "metric-example"
}

# A metric measures one tracked event name, exactly as the application reports
# it. Its kinds say what an experiment's results show for it: here, the share
# of exposed contexts that completed a checkout, and the order value per
# exposed context.
resource "configdirector_metric" "checkout_completed" {
  project_id  = configdirector_project.example.id
  event_name  = "checkout_completed"
  description = "A customer paid for their order."
  direction   = "higher-is-better"
  kinds       = ["conversion", "value-per-context"]
}

# A metric a team watches so that it does not get worse: failed checkouts per
# exposed context.
resource "configdirector_metric" "checkout_failed" {
  project_id = configdirector_project.example.id
  event_name = "checkout_failed"
  direction  = "lower-is-better"
  kinds      = ["count-per-context"]
}

# An experiment attaches metrics by id. One primary metric and kind decide the
# experiment; a guardrail must not get worse.
resource "configdirector_config" "checkout_button_copy" {
  project_id = configdirector_project.example.id
  key        = "checkout-button-copy"
  role       = "experiment"
  type       = "string"
  hypothesis = "Urgent copy gets more customers to check out."

  variations = [
    { name = "Control", value = "Buy now" },
    { name = "Urgency", value = "Buy now - limited stock" },
  ]

  metrics = [
    { metric_id = configdirector_metric.checkout_completed.id, role = "primary", primary_kind = "conversion" },
    { metric_id = configdirector_metric.checkout_failed.id, role = "guardrail" },
  ]

  initial_value = "Buy now"
}
