# <project_id_or_slug>/<config_key>/<environment_id_or_slug> - the project
# and the environment can each be identified by their UUID or their slug.
#
# Note: rules can't be recovered by import - it's write-only and never
# reconciled against a read. It reads as unset after import, and the first
# apply following import adopts whatever's configured as the new baseline.
terraform import configdirector_config_targeting_rules.beta_features_test targeting-rules-example/beta-features/test
