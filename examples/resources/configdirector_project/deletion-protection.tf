import {
  to = configdirector_project.storefront
  id = "storefront"
}

resource "configdirector_project" "storefront" {
  name = "Storefront"
  slug = "storefront"

  lifecycle {
    prevent_destroy = true
  }
}
