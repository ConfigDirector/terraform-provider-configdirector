# Terraform Provider for ConfigDirector

[![Build and Test](https://github.com/ConfigDirector/terraform-provider-configdirector/actions/workflows/build.yml/badge.svg)](https://github.com/ConfigDirector/terraform-provider-configdirector/actions/workflows/build.yml)
[![Terraform Registry](https://img.shields.io/badge/Terraform%20Registry-ConfigDirector%2Fconfigdirector-844FBA)](https://registry.terraform.io/providers/ConfigDirector/configdirector/latest)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL%202.0-brightgreen.svg)](LICENSE)

This is the official Terraform provider for [ConfigDirector](https://www.configdirector.com), remote config and
feature flags with typed values, JSON Schema validation, and safe renames of live flags. It manages projects,
environments, configs, segments, tags, and targeting rules as Terraform resources.

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
      version = "~> 0.2"
    }
  }
}

provider "configdirector" {
  # token defaults to the CONFIGDIRECTOR_TOKEN environment variable,
  # so this block can usually be left empty.
}
```

You can generate an admin API token via the ConfigDirector dashboard, then export the CONFIGDIRECTOR_TOKEN environment variable with
the API token.

A project with one feature flag looks like this:

```hcl
resource "configdirector_project" "example" {
  name = "Config Example"
  slug = "config-example"
}

resource "configdirector_config" "new_checkout_flow" {
  project_id  = configdirector_project.example.id
  key         = "new-checkout-flow"
  description = "Enables the redesigned checkout flow."
  role        = "flag"
  lifetime    = "temporary"
  type        = "boolean"

  initial_value = false
}
```

Full details are in the [official documentation](https://docs.configdirector.com/integrations/terraform).

See the [`examples/`](examples/) directory for a runnable example of every resource, data source, and function this
provider offers, and the [`docs/`](docs/) directory (or the [Registry documentation](https://registry.terraform.io/providers/ConfigDirector/configdirector/latest/docs))
for full schema reference.

## Managing the tag catalog with Terraform or OpenTofu

Use `configdirector_tag` to manage a shared project tag independently of configs:

```hcl
resource "configdirector_tag" "payments" {
  project_id = configdirector_project.example.id
  name       = "Payments / EU & US"
}
```

Renaming `name` updates the existing tag and preserves its ID. Refresh reads by ID, so a remote rename is
detected and reconciliation restores the declared name. Changing `project_id` replaces the tag. A deleted
tag is removed from state and can be recreated; deletion succeeds if the tag is already absent. Deleting a
shared tag also removes its assignments from active and archived configs. Creating a catalog tag does not
assign it to configs or affect evaluation.

Names are trimmed by the API, keep their display casing, and contain 1–50 characters after trimming.
Names can contain spaces and punctuation and are case-insensitively unique within a project. The provider
preserves the declared spelling when the API only trims surrounding whitespace. A duplicate name is an
error; import an existing tag to adopt it.

The API token needs `tags:read`, `tags:create`, `tags:update`, and `tags:delete` for the projects it manages.
Project resources separately require their project scopes. Importing with a project slug also needs
`projects:read` to find that project's ID; importing with a project UUID only needs `tags:read`.

Import uses `<project_id_or_slug>/<tag_name>`. The first `/` separates the project from the name;
everything after it is the literal tag name, including additional slashes. Do not URL-encode the name.
Quote the entire identifier in the shell:

```sh
terraform import configdirector_tag.payments 'tag-catalog-example/Payments / EU & US'
tofu import configdirector_tag.payments 'tag-catalog-example/Payments / EU & US'
```

Both tools can also use the import block shown in the [tag resource reference](docs/resources/tag.md).
Imports resolve the name through the API and save its stable ID. Subsequent reads and renames use that ID.
This resource manages catalog entries; config tag assignments and a standalone tag data source are outside
its scope.

The provider uses the same provider address and resource configuration in OpenTofu. See the
[tag example](examples/resources/configdirector_tag/resource.tf) for a complete configuration. This resource
can be released once the catalog API and tag permission parsing are deployed on every API instance; it
does not depend on config assignment/filter support or dashboard feature flags.

## Developing the provider

```sh
git clone git@github.com:ConfigDirector/terraform-provider-configdirector.git
cd terraform-provider-configdirector
make build
make vet
```

Acceptance tests run against a real ConfigDirector API, creating and destroying test resources. They are
gated behind `TF_ACC`. Tag tests call the Terraform Plugin Framework lifecycle and import methods directly,
without invoking Terraform plan/apply. Local tag validation tests run without an API token.

To run the tag API tests, set `CONFIGDIRECTOR_TOKEN` with the tag and project scopes above:

```sh
TF_ACC=1 go test ./internal/provider -run '^TestAccTagResource_' -count=1 -v
```

The existing acceptance harness for other resources invokes Terraform plan/apply, so do not run it where
those commands are prohibited. Where permitted, set `CONFIGDIRECTOR_TOKEN` and run:

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

## Getting Help

- [Ask a question in Discussions](https://github.com/orgs/ConfigDirector/discussions)
- [Contact support](https://www.configdirector.com/support)

## License

[Mozilla Public License 2.0](LICENSE)
