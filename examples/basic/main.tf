terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

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

resource "configdirector_environment" "test" {
  project_id = configdirector_project.example.id
  name       = "Test"
  slug       = "test"
  color      = "yellow"
  live       = false
}

resource "configdirector_config" "example_flag" {
  project_id = configdirector_project.example.id
  key        = "example-flag"
  role       = "flag"
  lifetime   = "temporary"
  type       = "boolean"

  # Write-only: the API never returns a config's default value, and changing
  # it after creation is a no-op (it can only be set at create time).
  initial_value = false
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
