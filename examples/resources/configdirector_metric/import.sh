# <project_id_or_slug>/<metric_id> - the project half accepts either its UUID
# or its slug. The metric's id ends the address of its page in the dashboard.
terraform import configdirector_metric.team_checkout_items metric-example/0b9d6c1e-58a4-4c39-9a7e-3f1c2d5e8b01

# <project_id_or_slug>/<event_name> - while the event name has one metric. Only
# the first slash separates the project from the event name, which is taken
# literally, letter case included. When several metrics share the event name,
# the import fails and lists their ids.
terraform import configdirector_metric.checkout_failed metric-example/checkout_failed

tofu import configdirector_metric.checkout_failed metric-example/checkout_failed
