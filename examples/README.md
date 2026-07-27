# Examples

Each subdirectory here is a self-contained, runnable example for one
resource:

- [project/](project/main.tf) - `configdirector_project`
- [environment/](environment/main.tf) - `configdirector_environment`
- [config/](config/main.tf) - `configdirector_config`, showing several
  role/type/type_options combinations
- [config_targeting_rules/](config_targeting_rules/main.tf) -
  `configdirector_config_targeting_rules`, showing multiple configs each
  with conditional and percentage-based targeting rules across environments

This provider isn't published to the Terraform registry, so Terraform needs
to be pointed at a locally-built binary via [dev overrides](https://developer.hashicorp.com/terraform/plugin/debugging#terraform-cli-development-overrides)
instead of running `terraform init`.

From the repo root:

```sh
make build
export CONFIGDIRECTOR_TOKEN=<your api token>
export TF_CLI_CONFIG_FILE="$PWD/examples/dev.tfrc"

cd examples/<resource>
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
