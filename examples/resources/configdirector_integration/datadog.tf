variable "datadog_api_key" {
  type      = string
  sensitive = true
}

resource "configdirector_integration" "datadog" {
  name = "Datadog"
  kind = "datadog"
  settings = {
    site    = "datadoghq.eu"
    api_key = var.datadog_api_key
  }
}
