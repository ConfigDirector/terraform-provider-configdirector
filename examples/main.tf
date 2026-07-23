terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

# host/token are left unset here: the provider falls back to the
# CONFIGDIRECTOR_HOST / CONFIGDIRECTOR_TOKEN environment variables.
provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Terraform Example"
  slug = "terraform-example"
}

resource "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  name       = "Staging"
  slug       = "staging"
  color      = "blue"
  live       = false
}

resource "configdirector_config" "example_flag" {
  project_id = configdirector_project.example.id
  key        = "example-flag"
  role       = "flag"
  lifetime   = "temporary"
  type       = "boolean"

  # JSON-encoded: the API's defaultValue field is a union type the
  # provider can't model natively, so it's passed through as a string.
  default_value = "false"
}

data "configdirector_environments" "all" {
  project_id = configdirector_project.example.id
  depends_on = [configdirector_environment.staging]
}

data "configdirector_configs" "all" {
  project_id = configdirector_project.example.id
  depends_on = [configdirector_config.example_flag]
}

output "project_id" {
  value = configdirector_project.example.id
}

output "environments" {
  value = data.configdirector_environments.all.environments
}

output "configs" {
  value = data.configdirector_configs.all.configs
}
