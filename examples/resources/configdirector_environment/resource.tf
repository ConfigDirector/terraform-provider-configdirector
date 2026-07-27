terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/ConfigDirector/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Environment Example"
  slug = "environment-example"
}

# Projects already come with "test" and "production" environments;
# configdirector_environment adds any others you need beyond those two.
resource "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  name       = "Staging"
  slug       = "staging"
  color      = "purple"
  live       = false
}

# "live" marks an environment as serving real traffic (as opposed to a
# pre-release environment like staging above) - it's informational only and
# doesn't otherwise change how Terraform manages the environment.
resource "configdirector_environment" "canary" {
  project_id = configdirector_project.example.id
  name       = "Canary"
  slug       = "canary"
  color      = "yellow"
  live       = true
}

output "staging_environment_id" {
  value = configdirector_environment.staging.id
}
