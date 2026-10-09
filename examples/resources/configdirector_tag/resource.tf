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
  name = "Tag Catalog Example"
  slug = "tag-catalog-example"
}

resource "configdirector_tag" "payments" {
  project_id = configdirector_project.example.id
  name       = "Payments / EU & US"
}

output "payments_tag_id" {
  value = configdirector_tag.payments.id
}
