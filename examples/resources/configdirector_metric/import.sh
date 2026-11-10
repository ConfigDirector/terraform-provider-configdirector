# <project_id_or_slug>/<event_name> - the project half accepts either its UUID
# or its slug. Only the first slash separates the project from the event name,
# which is taken literally, letter case included.
terraform import configdirector_metric.checkout_completed metric-example/checkout_completed

tofu import configdirector_metric.checkout_completed metric-example/checkout_completed
