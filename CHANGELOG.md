# Changelog

All notable changes to the ConfigDirector Terraform provider are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `configdirector_config` no longer requires `lifetime`. The role decides it for a flag, an experiment (both temporary) and a kill switch (permanent), so leave it out for those: the plan shows the lifetime the role decides. A lifetime that contradicts the role, such as a `kill-switch` with `lifetime = "temporary"`, now fails the plan with "A kill switch is always permanent." instead of failing the apply. A config with the `config` role still needs a lifetime when it is created, and the plan fails without one; leaving it out later keeps the lifetime it has.
- Changing a `configdirector_config` from `role = "flag"` to `"kill-switch"`, or back, updates the config in place, and with `lifetime` left out the plan shows the new lifetime. Any other role change now fails the plan, because ConfigDirector allows no other role change; before, it failed the apply.

## [0.5.0] - 2026-10-28

### Added

- `configdirector_webhook` manages a webhook with Terraform or OpenTofu: its name, `https://` URL, custom `headers`, whether it is `enabled`, its project, environment and tag filters, and the `event_types` and `alert_kinds` it receives. A webhook that leaves out `event_types` receives every change event type, `event_types = []` receives none, and one that leaves out `alert_kinds` receives no alert events. The signing `secret` is a sensitive read-only attribute to pass to your receiver; no plan shows it, and it stays the same across updates. `headers` is sensitive too. Because ConfigDirector never returns header values, a header added or removed outside Terraform shows up in the next plan, but a value changed there does not. Import a webhook by its ID; the first apply after an import writes the header values the configuration declares. The API token needs `webhooks:manage`.
