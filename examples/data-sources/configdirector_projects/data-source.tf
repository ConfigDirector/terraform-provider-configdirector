terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/ConfigDirector/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Projects Data Source Example"
  slug = "projects-data-source-example"
}

# Lists every project in the organization - not just the one above.
data "configdirector_projects" "all" {
  depends_on = [configdirector_project.example]
}

output "project_slugs" {
  value = [for p in data.configdirector_projects.all.projects : p.slug]
}
