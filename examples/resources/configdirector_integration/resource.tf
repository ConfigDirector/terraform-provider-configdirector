terraform {
  required_providers {
    configdirector = {
      source  = "ConfigDirector/configdirector"
      version = "~> 0.7"
    }
  }
}

provider "configdirector" {}

variable "amplitude_api_key" {
  type      = string
  sensitive = true
}

variable "amplitude_secret_key" {
  type      = string
  sensitive = true
}

resource "configdirector_project" "example" {
  name = "Integration Example"
  slug = "integration-example"
}

resource "configdirector_integration" "amplitude" {
  name = "Amplitude"
  kind = "amplitude"
  settings = {
    data_center = "us"
    api_key     = var.amplitude_api_key
    secret_key  = var.amplitude_secret_key
  }
  project_ids = [configdirector_project.example.id]
  environment_ids = [
    for environment in configdirector_project.example.environments : environment.id
    if environment.slug == "production"
  ]
}
