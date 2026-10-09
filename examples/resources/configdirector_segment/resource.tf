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
  name = "Segment Example"
  slug = "segment-example"
}

resource "configdirector_environment" "staging" {
  project_id = configdirector_project.example.id
  name       = "Staging"
  slug       = "staging"
  color      = "purple"
  live       = false
}

# A segment is a named, reusable test of a context, defined once per project.
# "groups" is a list of condition groups: a context is in the segment when
# every condition of any one group matches (AND inside a group, OR across
# groups). Conditions have the same shape as the attribute conditions of a
# targeting rule, and each needs its own stable id.
resource "configdirector_segment" "beta_testers" {
  project_id = configdirector_project.example.id
  key        = "beta-testers"
  name       = "Beta testers"

  groups = [
    # Anyone at Acme...
    [
      {
        id           = provider::configdirector::rule_id("beta-testers-acme-email")
        attribute    = "traits"
        trait        = "/email"
        operator     = "ends with any of"
        targetType   = "text"
        targetValues = ["@acme.com"]
      }
    ],
    # ...or anyone who opted in on a recent app version.
    [
      {
        id           = provider::configdirector::rule_id("beta-testers-opted-in")
        attribute    = "traits"
        trait        = "/betaOptIn"
        operator     = "equals"
        targetType   = "boolean"
        targetValues = ["true"]
      },
      {
        id           = provider::configdirector::rule_id("beta-testers-app-version")
        attribute    = "appVersion"
        operator     = ">="
        targetType   = "semver"
        targetValues = ["2.0.0"]
      }
    ]
  ]

  # An environment override replaces the whole definition in that
  # environment. Here staging treats a couple of named users as beta
  # testers instead; every other environment uses the groups above.
  overrides = {
    (configdirector_environment.staging.id) = [
      [
        {
          id           = provider::configdirector::rule_id("beta-testers-staging-named-users")
          attribute    = "identifier"
          operator     = "is one of"
          targetType   = "text"
          targetValues = ["user-101", "user-202"]
        }
      ]
    ]
  }
}

resource "configdirector_config" "new_checkout_flow" {
  project_id    = configdirector_project.example.id
  key           = "new-checkout-flow"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}

# Targeting rules use a segment through a segment condition that references
# the segment by id. A rule can mix segment conditions with attribute
# conditions; all of a rule's conditions must match.
resource "configdirector_config_targeting_rules" "new_checkout_flow_production" {
  project_id       = configdirector_project.example.id
  config_key       = configdirector_config.new_checkout_flow.key
  environment_slug = "production"
  default_value    = "false"

  rules = [
    {
      id     = provider::configdirector::rule_id("new-checkout-flow-production-beta-testers")
      type   = "conditional"
      order  = 0
      target = "value"
      value  = true
      conditions = [
        {
          id        = provider::configdirector::rule_id("new-checkout-flow-production-beta-testers-segment")
          kind      = "segment"
          operator  = "in"
          segmentId = configdirector_segment.beta_testers.id
        }
      ]
    }
  ]
}

output "beta_testers_segment_id" {
  value = configdirector_segment.beta_testers.id
}
