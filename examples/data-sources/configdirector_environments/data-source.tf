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
  name = "Environments Data Source Example"
  slug = "environments-data-source-example"
}

resource "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  name       = "Staging"
  slug       = "staging"
  color      = "purple"
  live       = false
}

# Includes the "test"/"production" environments the project was created
# with, plus "staging" above.
data "configdirector_environments" "all" {
  project_id = configdirector_project.example.id
  depends_on = [configdirector_environment.staging]
}

output "environment_slugs" {
  value = [for e in data.configdirector_environments.all.environments : e.slug]
}
