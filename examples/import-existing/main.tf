terraform {
  required_providers {
    configdirector = {
      source = "registry.terraform.io/alejandro/configdirector"
    }
  }
}

provider "configdirector" {}

resource "configdirector_project" "terraform-import" {
  name = "Terraform Import"
  slug = "terraform-import"
}

import {
  to = configdirector_project.terraform-import
  id = "terraform-import"
}