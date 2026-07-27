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
    },
    {
      value = "Four"
    }
  ]

  initial_value = "Two"
}

resource "configdirector_config_targeting_rules" "example_experiment_test_rules" {
  project_id = configdirector_project.example.id
  config_key = configdirector_config.example_experiment.key
  environment_slug = "test"
  default_value = "Two"
  rules = [
    {
      # Conditional rule: VIP test users always get "Three", regardless of
      # the rollout below.
      id     = provider::configdirector::rule_id("example-experiment-test-vip-users")
      type   = "conditional"
      order  = 0
      target = "value"
      value  = "Three"
      conditions = [
        {
          id           = provider::configdirector::rule_id("example-experiment-test-vip-users-identifier")
          attribute    = "identifier"
          operator     = "is one of"
          targetType   = "text"
          targetValues = ["user-123", "user-457"]
        }
      ]
    },
    {
      # Percentage rollout for everyone else: 50/30/20 split across the
      # config's three variations.
      id     = provider::configdirector::rule_id("example-experiment-test-rollout")
      type   = "percentage"
      order  = 1
      target = "percentage"
      percentages = [
        {
          id         = provider::configdirector::rule_id("example-experiment-test-rollout-one")
          percentage = 50
          value      = "One"
        },
        {
          id         = provider::configdirector::rule_id("example-experiment-test-rollout-two")
          percentage = 30
          value      = "Two"
        },
        {
          id         = provider::configdirector::rule_id("example-experiment-test-rollout-three")
          percentage = 20
          value      = "Four"
        },
      ]
    },
  ]
}