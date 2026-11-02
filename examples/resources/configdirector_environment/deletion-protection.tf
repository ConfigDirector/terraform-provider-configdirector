import {
  to = configdirector_project.storefront
  id = "storefront"
}

import {
  to = configdirector_environment.production
  id = "storefront/production"
}

resource "configdirector_project" "storefront" {
  name = "Storefront"
  slug = "storefront"
}

resource "configdirector_environment" "production" {
  project_id = configdirector_project.storefront.id
  name       = "Production"
  slug       = "production"
  color      = "red"
  live       = true

  lifecycle {
    prevent_destroy = true
  }
}
