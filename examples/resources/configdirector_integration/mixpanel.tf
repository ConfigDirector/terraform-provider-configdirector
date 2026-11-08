variable "mixpanel_service_account_username" {
  type      = string
  sensitive = true
}

variable "mixpanel_service_account_secret" {
  type      = string
  sensitive = true
}

resource "configdirector_integration" "mixpanel" {
  name = "Mixpanel"
  kind = "mixpanel"
  settings = {
    data_residency           = "eu"
    project_id               = "3212345"
    service_account_username = var.mixpanel_service_account_username
    service_account_secret   = var.mixpanel_service_account_secret
  }
}
