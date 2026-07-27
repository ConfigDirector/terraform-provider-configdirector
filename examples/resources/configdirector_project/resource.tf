terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/ConfigDirector/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Project Example"
  slug = "project-example"
}

# Every project is created with "test" and "production" environments
# automatically - the API creates them, not Terraform, so they're surfaced
# here as read-only data rather than as separate resources. See the
# configdirector_environment example for adding more environments to a
# project.
output "environments" {
  value = configdirector_project.example.environments
}

output "organization_id" {
  value = configdirector_project.example.organization_id
}
