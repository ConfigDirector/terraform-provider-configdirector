terraform {
  required_providers {
    configdirector = {
      source  = "registry.terraform.io/ConfigDirector/configdirector"
      version = "~> 0.1"
    }
  }
}

# token defaults to environment variable CONFIGDIRECTOR_TOKEN when not
# set here, so the block can usually be left empty like this.
# Avoid committing the token credential to source control.
provider "configdirector" {}
