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

resource "configdirector_config_targeting_rules" "example_flag_test_rules" {
  project_id = configdirector_project.example.id
  config_key = configdirector_config.example_flag.key
  environment_slug = "test"
  default_value = true
}

resource "configdirector_config_targeting_rules" "example_flag_prod_rules" {
  project_id = configdirector_project.example.id
  config_key = configdirector_config.example_flag.key
  environment_slug = "production"
  default_value = false
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