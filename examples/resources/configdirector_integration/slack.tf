variable "slack_incidents_webhook_url" {
  type      = string
  sensitive = true
}

resource "configdirector_integration" "incidents" {
  name = "#incidents"
  kind = "slack"
  settings = {
    webhook_url    = var.slack_incidents_webhook_url
    message_prefix = "@here"
  }
  event_types = []
  alert_kinds = [
    "default-value-served-too-often",
    "experiment-guardrail-worse",
    "integration-disabled",
    "webhook-disabled",
  ]
  minimum_priority = "critical"
}
