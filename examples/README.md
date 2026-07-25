# Example usage

This provider isn't published to the Terraform registry, so Terraform needs
to be pointed at a locally-built binary via [dev overrides](https://developer.hashicorp.com/terraform/plugin/debugging#terraform-cli-development-overrides)
instead of running `terraform init`.

From the repo root:

```sh
make build
export CONFIGDIRECTOR_TOKEN=<your api token>
export TF_CLI_CONFIG_FILE="$PWD/examples/dev.tfrc"

cd examples
terraform plan
terraform apply
```

Skip `terraform init` — dev overrides bypass provider installation entirely,
and `init` isn't needed for a single-provider config like this one. Terraform
will print a "development overrides are in effect" warning on every run;
that's expected.

By default the provider talks to `https://api.configdirector.com`. Set
`CONFIGDIRECTOR_BASE_URL` to point elsewhere.

Run `terraform destroy` when you're done to clean up the example project,
environment, and config.

To import an existing environment:

```sh
terraform import configdirector_environment.test "$(terraform output -raw project_id)/test"
```
