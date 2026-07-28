# <project_id>/<config_key>/<environment_id_or_slug> - the environment can
# be identified by its UUID or its slug.
#
# Note: rules can't be recovered by import - it's write-only and never
# reconciled against a read. It reads as unset after import, and the first 
# apply following import adopts whatever's configured as the new baseline.
terraform import configdirector_config_targeting_rules.beta_features_test 0198c1b2-3a4b-7c1d-8e2f-1a2b3c4d5e6f/beta-features/test
