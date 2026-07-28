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
  name = "Project Data Source Example"
  slug = "project-data-source-example"
}

data "configdirector_project" "example" {
  id = configdirector_project.example.id
}

output "project_environments" {
  value = data.configdirector_project.example.environments
}
