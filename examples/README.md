# Examples

This directory follows the standard [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs)
layout, which also doubles as documentation source: each `.tf` file here can
get pulled directly into the generated provider docs.

- [provider/provider.tf](provider/provider.tf) - configuring the provider
  itself
- `resources/<resource_type>/resource.tf` - one directory per resource, each
  a self-contained, runnable example:
  - [configdirector_project](resources/configdirector_project/resource.tf)
  - [configdirector_environment](resources/configdirector_environment/resource.tf)
  - [configdirector_config](resources/configdirector_config/resource.tf) -
    several role/type/type_options combinations
  - [configdirector_config_targeting_rules](resources/configdirector_config_targeting_rules/resource.tf) -
    multiple configs, each with conditional and percentage-based targeting
    rules across environments
- `data-sources/<data_source_type>/data-source.tf` - one directory per data
  source, each creating whatever resource(s) it needs to look up:
  - [configdirector_project](data-sources/configdirector_project/data-source.tf)
  - [configdirector_projects](data-sources/configdirector_projects/data-source.tf)
  - [configdirector_environment](data-sources/configdirector_environment/data-source.tf)
  - [configdirector_environments](data-sources/configdirector_environments/data-source.tf)
  - [configdirector_config](data-sources/configdirector_config/data-source.tf)
  - [configdirector_configs](data-sources/configdirector_configs/data-source.tf)

This provider isn't published to the Terraform registry, so Terraform needs
to be pointed at a locally-built binary via [dev overrides](https://developer.hashicorp.com/terraform/plugin/debugging#terraform-cli-development-overrides)
instead of running `terraform init`.

From the repo root:

```sh
make build
export CONFIGDIRECTOR_TOKEN=<your api token>
export TF_CLI_CONFIG_FILE="$PWD/examples/dev.tfrc"

cd examples/resources/<resource_type>   # or data-sources/<data_source_type>, or provider
terraform plan
terraform apply
```

Skip `terraform init` - dev overrides bypass provider installation entirely,
and `init` isn't needed for these single-provider configs. Terraform will
print a "development overrides are in effect" warning on every run; that's
expected.

By default the provider talks to `https://api.configdirector.com`. Set
`CONFIGDIRECTOR_BASE_URL` to point elsewhere.

Run `terraform destroy` when you're done with an example to clean up the
project (and everything under it) it created.
