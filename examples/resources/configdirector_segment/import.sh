# <project_id_or_slug>/<segment_key> - the project half accepts either its
# UUID or its slug. groups and overrides are write-only (never read back from
# the API), so they can't be recovered by import: the first apply after
# importing sends whatever the configuration declares.
terraform import configdirector_segment.beta_testers segment-example/beta-testers
