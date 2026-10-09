terraform {
  required_providers {
    configdirector = {
      source  = "ConfigDirector/configdirector"
      version = "~> 0.2"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Configs Data Source Example"
  slug = "configs-data-source-example"
}

resource "configdirector_config" "example" {
  project_id    = configdirector_project.example.id
  key           = "example-flag"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
  tags          = [configdirector_tag.payments.name]
}

resource "configdirector_tag" "payments" {
  project_id = configdirector_project.example.id
  name       = "Payments"
}

data "configdirector_configs" "all" {
  project_id = configdirector_project.example.id
  depends_on = [configdirector_config.example]
}

output "config_keys" {
  value = [for c in data.configdirector_configs.all.configs : c.key]
}

output "assigned_tag_names" {
  value = { for c in data.configdirector_configs.all.configs : c.key => c.tags }
}
