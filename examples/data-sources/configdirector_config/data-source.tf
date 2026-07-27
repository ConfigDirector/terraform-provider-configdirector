terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/ConfigDirector/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Config Data Source Example"
  slug = "config-data-source-example"
}

resource "configdirector_config" "example" {
  project_id    = configdirector_project.example.id
  key           = "example-flag"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}

data "configdirector_config" "example" {
  project_id = configdirector_project.example.id
  key        = configdirector_config.example.key
}

output "config_state" {
  value = data.configdirector_config.example.state
}
