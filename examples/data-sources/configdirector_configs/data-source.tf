terraform {
  required_providers {
    configdirector = {
      source  = "registry.terraform.io/ConfigDirector/configdirector"
      version = "~> 0.1"
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
}

data "configdirector_configs" "all" {
  project_id = configdirector_project.example.id
  depends_on = [configdirector_config.example]
}

output "config_keys" {
  value = [for c in data.configdirector_configs.all.configs : c.key]
}
