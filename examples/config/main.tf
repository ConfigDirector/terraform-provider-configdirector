terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "example" {
  name = "Config Example"
  slug = "config-example"
}

# A simple boolean flag - the most common case.
resource "configdirector_config" "new_checkout_flow" {
  project_id  = configdirector_project.example.id
  key         = "new-checkout-flow"
  description = "Enables the redesigned checkout flow."
  role        = "flag"
  lifetime    = "temporary"
  type        = "boolean"

  # Write-only: the API never returns a config's default value, and
  # changing it after creation is a no-op - it can only be set at create
  # time. Ongoing default value changes go through
  # configdirector_config_targeting_rules instead (see that resource's
  # example).
  initial_value = false
}

# A kill-switch, visible to both client and server SDKs (the default for
# both, shown explicitly here).
resource "configdirector_config" "maintenance_mode" {
  project_id  = configdirector_project.example.id
  key         = "maintenance-mode"
  description = "Puts the app into maintenance mode when enabled."
  role        = "kill-switch"
  lifetime    = "permanent"
  type        = "boolean"
  client      = true
  server      = true

  initial_value = false
}

# An integer config, bounded via type_options. type_options is shaped
# differently depending on "type" (see the resource documentation for the
# other shapes) and isn't validated by Terraform - it's passed through
# as-is and validated by the API.
resource "configdirector_config" "max_upload_size_mb" {
  project_id = configdirector_project.example.id
  key        = "max-upload-size-mb"
  role       = "config"
  lifetime   = "permanent"
  type       = "integer"

  type_options = {
    isInteger = true
    min = {
      relation = ">="
      value    = 1
    }
    max = {
      relation = "<="
      value    = 500
    }
  }

  initial_value = 25
}

# An enum config, restricted to a fixed set of values via type_options.
resource "configdirector_config" "checkout_theme" {
  project_id = configdirector_project.example.id
  key        = "checkout-theme"
  role       = "config"
  lifetime   = "permanent"
  type       = "enum"

  type_options = {
    valueType = "string"
    values    = ["classic", "modern", "minimal"]
  }

  initial_value = "classic"
}

# An experiment with named variations - see
# configdirector_config_targeting_rules for how traffic actually gets split
# between them.
resource "configdirector_config" "pricing_experiment" {
  project_id = configdirector_project.example.id
  key        = "pricing-experiment"
  role       = "experiment"
  lifetime   = "temporary"
  type       = "string"

  variations = [
    { name = "Control", value = "control" },
    { name = "Higher Price", value = "higher-price" },
    { name = "Lower Price", value = "lower-price" },
  ]

  initial_value = "control"
}
