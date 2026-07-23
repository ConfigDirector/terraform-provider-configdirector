terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

# host/token are left unset here: the provider falls back to the
# CONFIGDIRECTOR_HOST / CONFIGDIRECTOR_TOKEN environment variables.
provider "configdirector" {}

resource "configdirector_project" "terraform-import" {
  name = "Terraform Import"
  slug = "terraform-import"
}

import {
  to = configdirector_project.terraform-import
  id = "terraform-import"
}