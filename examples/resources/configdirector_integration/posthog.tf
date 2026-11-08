variable "posthog_personal_api_key" {
  type      = string
  sensitive = true
}

resource "configdirector_integration" "posthog" {
  name = "PostHog"
  kind = "posthog"
  settings = {
    host             = "eu"
    project_id       = "42"
    personal_api_key = var.posthog_personal_api_key
  }
}
