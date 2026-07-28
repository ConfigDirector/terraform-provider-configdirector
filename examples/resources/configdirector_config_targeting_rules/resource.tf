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
  name = "Targeting Rules Example"
  slug = "targeting-rules-example"
}

# --- A boolean flag: named-user override in "test", percentage rollout in
#     "production" ---

resource "configdirector_config" "beta_features" {
  project_id    = configdirector_project.example.id
  key           = "beta-features"
  role          = "flag"
  lifetime      = "temporary"
  type          = "boolean"
  initial_value = false
}

# In "test", the flag defaults to off, except for a couple of named beta
# testers who always get it on regardless of the default.
resource "configdirector_config_targeting_rules" "beta_features_test" {
  project_id       = configdirector_project.example.id
  config_key       = configdirector_config.beta_features.key
  environment_slug = "test"
  default_value    = "false"

  rules = [
    {
      id     = provider::configdirector::rule_id("beta-features-test-named-testers")
      type   = "conditional"
      order  = 0
      target = "value"
      value  = true
      conditions = [
        {
          id           = provider::configdirector::rule_id("beta-features-test-named-testers-identifier")
          attribute    = "identifier"
          operator     = "is one of"
          targetType   = "text"
          targetValues = ["user-101", "user-202"]
        }
      ]
    }
  ]
}

# In "production", the flag is on for 10% of traffic.
resource "configdirector_config_targeting_rules" "beta_features_production" {
  project_id       = configdirector_project.example.id
  config_key       = configdirector_config.beta_features.key
  environment_slug = "production"
  default_value    = "false"

  rules = [
    {
      id     = provider::configdirector::rule_id("beta-features-production-rollout")
      type   = "percentage"
      order  = 0
      target = "percentage"
      percentages = [
        {
          id         = provider::configdirector::rule_id("beta-features-production-rollout-on")
          percentage = 10
          value      = true
        },
        {
          id         = provider::configdirector::rule_id("beta-features-production-rollout-off")
          percentage = 90
          value      = false
        },
      ]
    }
  ]
}

# --- A three-way experiment: VIP override plus a percentage split,
#     combined in a single rules list (order matters - the first matching
#     rule wins) ---

resource "configdirector_config" "checkout_button_copy" {
  project_id = configdirector_project.example.id
  key        = "checkout-button-copy"
  role       = "experiment"
  lifetime   = "temporary"
  type       = "string"

  variations = [
    { name = "Control", value = "Buy now" },
    { name = "Urgency", value = "Buy now - limited stock" },
    { name = "Friendly", value = "Add to your order" },
  ]

  initial_value = "Buy now"
}

# "test" just uses the plain default - no rules needed here.
resource "configdirector_config_targeting_rules" "checkout_button_copy_test" {
  project_id       = configdirector_project.example.id
  config_key       = configdirector_config.checkout_button_copy.key
  environment_slug = "test"
  default_value    = "Buy now"
}

resource "configdirector_config_targeting_rules" "checkout_button_copy_production" {
  project_id       = configdirector_project.example.id
  config_key       = configdirector_config.checkout_button_copy.key
  environment_slug = "production"
  default_value    = "Buy now"

  rules = [
    {
      # VIP customers always see the friendly copy, regardless of the
      # rollout below. Evaluated first (order = 0).
      id     = provider::configdirector::rule_id("checkout-copy-production-vip")
      type   = "conditional"
      order  = 0
      target = "value"
      value  = "Add to your order"
      conditions = [
        {
          id           = provider::configdirector::rule_id("checkout-copy-production-vip-identifier")
          attribute    = "identifier"
          operator     = "is one of"
          targetType   = "text"
          targetValues = ["user-123", "user-457"]
        }
      ]
    },
    {
      # Everyone else is split across the three variations.
      id     = provider::configdirector::rule_id("checkout-copy-production-rollout")
      type   = "percentage"
      order  = 1
      target = "percentage"
      percentages = [
        {
          id         = provider::configdirector::rule_id("checkout-copy-production-rollout-control")
          percentage = 50
          value      = "Buy now"
        },
        {
          id         = provider::configdirector::rule_id("checkout-copy-production-rollout-urgency")
          percentage = 30
          value      = "Buy now - limited stock"
        },
        {
          id         = provider::configdirector::rule_id("checkout-copy-production-rollout-friendly")
          percentage = 20
          value      = "Add to your order"
        },
      ]
    },
  ]
}
