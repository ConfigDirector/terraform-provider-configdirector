terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/ConfigDirector/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Environment Data Source Example"
  slug = "environment-data-source-example"
}

resource "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  name       = "Staging"
  slug       = "staging"
  color      = "purple"
  live       = false
}

data "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  id         = configdirector_environment.staging.id
}

output "environment_color" {
  value = data.configdirector_environment.staging.color
}
