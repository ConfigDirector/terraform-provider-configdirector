# Changelog

All notable changes to the ConfigDirector Terraform provider are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.7.0] - 2026-11-08

### Added

- `configdirector_integration` manages an integration with Amplitude, Mixpanel, Datadog, Grafana or PostHog, which marks experiment runs on the tool's charts, from Terraform or OpenTofu. Set its `kind`, the `settings` that kind needs, including the optional `endpoint_url`, whether it is `enabled`, and its project, environment and tag filters. Keys, tokens and secrets are sensitive, so no plan shows them. Leaving out a setting the kind needs, or leaving it blank, fails the plan with a message saying what is missing, such as "The API key is required", and a setting another kind uses fails the plan too. ConfigDirector never returns a key, token or secret, so changing one in the configuration saves all of the integration's credentials on the next apply, and replacing one in the dashboard goes unnoticed. Changing `kind` replaces the integration, which deletes its delivery log. Import an integration by its ID; an import reads everything except the keys, tokens and secrets, so the first apply after it saves the ones in your configuration. The API token needs `integrations:manage`.

### Changed

- A `configdirector_webhook` that leaves out `event_types` now receives every change event type, including the four `experiment_run.*` types, the same default as the API and the dashboard. The next plan for such a webhook shows `event_types` gaining those four. A webhook that lists its `event_types` keeps that list.

## [0.6.3] - 2026-11-03

### Fixed

- A plan or apply no longer fails the first time the network drops a connection to ConfigDirector, as in `read: connection reset by peer`. When a connection cannot be opened, the provider tries again up to twice, half a second and then a second later. When the network breaks a connection after a request went out, it tries again only for requests that read, replace or delete; the others, such as creating a project, still fail, because ConfigDirector may already have received them.

## [0.6.2] - 2026-11-02

### Fixed

- A `configdirector_config` experiment, such as an `integer`, `float` or `boolean` one, now applies and refreshes without errors when its `variations` values are written as numbers or booleans, such as `value = 10` or `value = true`. ConfigDirector stores every variation value as text, and the apply used to fail with "Provider produced inconsistent result after apply". Terraform now keeps each value as you wrote it, as long as the value in ConfigDirector stays the same; a value changed outside Terraform still shows up in the next plan. A boolean experiment's `variations` still have to be the two ConfigDirector creates, `{ name = "Enabled", value = true }` and `{ name = "Disabled", value = false }`, in that order. Importing an experiment stores its values as text, such as `"10"`, so the first plan after the import shows them changing to the numbers or booleans in your configuration.

## [0.6.1] - 2026-11-02

### Added

- `configdirector_project` and `configdirector_environment` read `deletion_protection`, which says whether deletion protection is on. It is turned on and off in the ConfigDirector dashboard, so Terraform only reads it and never plans a change to it. While it is on, destroying the project or environment fails with an error that says where to turn it off, and so does destroying a project with a protected environment. Add `lifecycle { prevent_destroy = true }` to a protected project or environment to keep Terraform from planning its destroy. The `configdirector_project`, `configdirector_projects`, `configdirector_environment` and `configdirector_environments` data sources read it too, as does each environment in a project's `environments`.

## [0.6.0] - 2026-11-01

### Changed

- `configdirector_config` no longer requires `lifetime`. The role decides it for a flag, an experiment (both temporary) and a kill switch (permanent), so leave it out for those: the plan shows the lifetime the role decides. A lifetime that contradicts the role, such as a `kill-switch` with `lifetime = "temporary"`, now fails the plan with "A kill switch is always permanent." instead of failing the apply. A config with the `config` role still needs a lifetime when it is created, and the plan fails without one; leaving it out later keeps the lifetime it has.
- Changing a `configdirector_config` from `role = "flag"` to `"kill-switch"`, or back, updates the config in place, and with `lifetime` left out the plan shows the new lifetime. Any other role change now fails the plan, because ConfigDirector allows no other role change; before, it failed the apply.

## [0.5.0] - 2026-10-28

### Added

- `configdirector_webhook` manages a webhook with Terraform or OpenTofu: its name, `https://` URL, custom `headers`, whether it is `enabled`, its project, environment and tag filters, and the `event_types` and `alert_kinds` it receives. A webhook that leaves out `event_types` receives every change event type, `event_types = []` receives none, and one that leaves out `alert_kinds` receives no alert events. The signing `secret` is a sensitive read-only attribute to pass to your receiver; no plan shows it, and it stays the same across updates. `headers` is sensitive too. Because ConfigDirector never returns header values, a header added or removed outside Terraform shows up in the next plan, but a value changed there does not. Import a webhook by its ID; the first apply after an import writes the header values the configuration declares. The API token needs `webhooks:manage`.
