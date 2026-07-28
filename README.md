# Terraform Provider for ConfigDirector

[![Build and Test](https://github.com/ConfigDirector/terraform-provider-configdirector/actions/workflows/build.yml/badge.svg)](https://github.com/ConfigDirector/terraform-provider-configdirector/actions/workflows/build.yml)
[![Terraform Registry](https://img.shields.io/badge/Terraform%20Registry-ConfigDirector%2Fconfigdirector-844FBA)](https://registry.terraform.io/providers/ConfigDirector/configdirector/latest)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL%202.0-brightgreen.svg)](LICENSE)

This is the official Terraform provider for [ConfigDirector](https://www.configdirector.com), letting you manage
projects, environments, configs, and feature-flag/config targeting rules as Terraform resources.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://go.dev/doc/install) >= 1.26 (only needed to build the provider locally)

## Using the provider

The provider is published on the [Terraform Registry](https://registry.terraform.io/providers/ConfigDirector/configdirector/latest).
Add it to your configuration and run `terraform init`:

```hcl
terraform {
  required_providers {
    configdirector = {
      source  = "registry.terraform.io/ConfigDirector/configdirector"
      version = "~> 0.1"
    }
  }
}

provider "configdirector" {
  # base_url and token both default to the CONFIGDIRECTOR_BASE_URL and
  # CONFIGDIRECTOR_TOKEN environment variables, respectively, so this block
  # can usually be left empty.
}
```

See the [`examples/`](examples/) directory for a runnable example of every resource, data source, and function this
provider offers, and the [`docs/`](docs/) directory (or the [Registry documentation](https://registry.terraform.io/providers/ConfigDirector/configdirector/latest/docs))
for full schema reference.

## Developing the provider

```sh
git clone git@github.com:ConfigDirector/terraform-provider-configdirector.git
cd terraform-provider-configdirector
make build
make vet
```

There's no hermetic/mocked test layer - every test is an acceptance test that runs against a real ConfigDirector API
(creating and destroying real resources), gated behind `TF_ACC` so it never runs by accident. Set
`CONFIGDIRECTOR_TOKEN` and run:

```sh
export CONFIGDIRECTOR_TOKEN=<your api token>
make test_integration
```

Regenerate documentation after changing a resource/data source schema or an example under `examples/`:

```sh
make docs
```

See [`examples/README.md`](examples/README.md) for how to run the examples against a locally-built binary via dev
overrides, instead of the published release.

## Releasing

Releases are cut by pushing a `v*` tag (e.g. `v0.2.0`). The [release workflow](.github/workflows/release.yml) runs
the full build-and-test suite as a gate, then builds, signs, and publishes the release via
[GoReleaser](https://goreleaser.com), which the Terraform Registry picks up automatically.

## License

[Mozilla Public License 2.0](LICENSE)
