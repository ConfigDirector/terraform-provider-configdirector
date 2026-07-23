terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Terraform Targeting Rules Example"
  slug = "terraform-targeting-rules"
}

resource "configdirector_config" "example_flag" {
  project_id = configdirector_project.example.id
  key        = "example-flag"
  role       = "flag"
  lifetime   = "temporary"
  type       = "boolean"

  default_value = "false"
}

