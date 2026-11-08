variable "grafana_service_account_token" {
  type      = string
  sensitive = true
}

resource "configdirector_integration" "grafana" {
  name = "Grafana"
  kind = "grafana"
  settings = {
    url                   = "https://example.grafana.net"
    service_account_token = var.grafana_service_account_token
  }
}
