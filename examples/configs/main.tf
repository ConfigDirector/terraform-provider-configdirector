terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Terraform Config & Targeting Rules Example"
  slug = "terraform-config-example"
}

import {
  to = configdirector_project.example
  id = "terraform-config-example"
}

resource "configdirector_config" "example_flag" {
  project_id = configdirector_project.example.id
  key        = "example-flag"
  role       = "flag"
  lifetime   = "temporary"
  type       = "boolean"

  initial_value = false
}

resource "configdirector_config" "example_experiment" {
  project_id = configdirector_project.example.id
  key        = "example-experiment"
  role       = "experiment"
  lifetime   = "temporary"
  type       = "string"

  variations = [
    {
      name = "Variation One"
      value = "One"
    },
    {
      value = "Two"
    },
    {
      value = "Three"
    }
  ]

  initial_value = "Two"
}