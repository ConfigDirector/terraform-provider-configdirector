terraform {
  required_providers {
    configdirector = {
      source  = "registry.terraform.io/ConfigDirector/configdirector"
      version = "~> 0.1"
    }
  }
}

# base_url and token both default to environment variables
# (CONFIGDIRECTOR_BASE_URL and CONFIGDIRECTOR_TOKEN, respectively) when not
# set here, so the block can usually be left empty like this - especially
# for token, to avoid committing a credential to source control.
provider "configdirector" {}
