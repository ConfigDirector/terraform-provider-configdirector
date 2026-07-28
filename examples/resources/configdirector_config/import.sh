# <project_id>/<key>
#
# Note: initial_value can't be recovered by import - the API never returns
# a config's default value, so it reads as unset afterward, and the first
# apply following import adopts whatever's configured as the new baseline
# rather than actually changing anything remotely. Make sure it's set to
# the value you want treated as the baseline going forward before that
# first apply.
terraform import configdirector_config.new_checkout_flow 0198c1b2-3a4b-7c1d-8e2f-1a2b3c4d5e6f/new-checkout-flow
