# <project_id_or_slug>/<environment_id_or_slug> - both halves accept either
# form: the project, its UUID or its slug; the environment, its UUID or its
# slug, e.g. "test"/"production" (the two environments every project is
# created with).
terraform import configdirector_environment.staging environment-example/staging
