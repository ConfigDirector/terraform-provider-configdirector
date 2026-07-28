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
- `functions/<function_name>/function.tf` - one directory per
  provider-defined function:
  - [rule_id](functions/rule_id/function.tf)

Every resource above also has, alongside its `resource.tf`, two files documenting `terraform import` for it:

- `import.sh` - the [`terraform import` command](https://developer.hashicorp.com/terraform/cli/commands/import),
  showing the exact composite ID format that resource expects (several of these IDs are `/`-joined composites, e.g.
  `<project_id>/<key>` for `configdirector_config` - see each file's comments for the exact shape and any caveats,
  like which attributes can't be recovered by import at all)
- `import-by-string-id.tf` - the same import, as a Terraform >= 1.5
  [`import` block](https://developer.hashicorp.com/terraform/language/import)

Both are documentation snippets - `tfplugindocs` picks them up automatically and renders them into each resource's
"Import" doc section, matching every other provider's `examples/resources/` layout - **not runnable configuration**.
This matters because Terraform merges every `.tf` file in a directory: **`terraform apply` will fail** in any
`resources/<resource_type>/` directory as long as `import-by-string-id.tf` is present, since it tries to import
before `resource.tf` has created anything to import. Delete or move that one file out of the way first if you want
to actually run `resource.tf` (see below); `import.sh` is inert since Terraform doesn't parse `.sh` files.

The provider is published at [registry.terraform.io/ConfigDirector/configdirector](https://registry.terraform.io/providers/ConfigDirector/configdirector),
so every other example (and `resource.tf` files once `import-by-string-id.tf` is out of the way) installs normally:

```sh
export CONFIGDIRECTOR_TOKEN=<your api token>

cd examples/resources/<resource_type>   # or data-sources/<data_source_type>, functions/<function_name>, or provider
terraform init
terraform plan
terraform apply
```

By default the provider talks to `https://api.configdirector.com`. Set
`CONFIGDIRECTOR_BASE_URL` to point elsewhere.

Run `terraform destroy` when you're done with an example to clean up the
project (and everything under it) it created.

## Testing against a local, unreleased build

To run an example against a locally-built binary instead of the published
release (e.g. to try out an unreleased change before tagging), point
Terraform at it via [dev overrides](https://developer.hashicorp.com/terraform/plugin/debugging#terraform-cli-development-overrides):

```sh
make build
export CONFIGDIRECTOR_TOKEN=<your api token>
export TF_CLI_CONFIG_FILE="$PWD/examples/dev.tfrc"

cd examples/resources/<resource_type>
terraform plan   # no `terraform init` - dev overrides bypass it entirely
terraform apply
```

Terraform will print a "development overrides are in effect" warning on
every run; that's expected. The version constraint in each example's
`required_providers` block is ignored under dev overrides, so this works
regardless of what's published.
