terraform {
  required_providers {
    configdirector = {
      source  = "ConfigDirector/configdirector"
      version = "~> 0.2"
    }
  }
}

# token defaults to environment variable CONFIGDIRECTOR_TOKEN when not
# set here, so the block can usually be left empty like this.
# Avoid committing the token credential to source control.
provider "configdirector" {}
