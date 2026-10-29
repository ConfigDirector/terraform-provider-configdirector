terraform {
  required_providers {
    configdirector = {
      source  = "ConfigDirector/configdirector"
      version = "~> 0.5"
    }
  }
}

provider "configdirector" {}

variable "ci_token" {
  type      = string
  sensitive = true
}

resource "configdirector_project" "example" {
  name = "Webhook Example"
  slug = "webhook-example"
}

resource "configdirector_webhook" "ci" {
  name        = "CI pipeline"
  url         = "https://example.com/configdirector-webhooks"
  project_ids = [configdirector_project.example.id]
  environment_ids = [
    for environment in configdirector_project.example.environments : environment.id
    if environment.slug == "production"
  ]
  headers = {
    Authorization = "Bearer ${var.ci_token}"
  }
}

module "ci_receiver" {
  source         = "./ci-receiver"
  signing_secret = configdirector_webhook.ci.secret
}

resource "configdirector_webhook" "incidents" {
  name        = "Incident tool"
  url         = "https://example.com/configdirector-alerts"
  event_types = []
  alert_kinds = ["default-value-mismatch", "config-type-mismatch", "config-settled"]
}
