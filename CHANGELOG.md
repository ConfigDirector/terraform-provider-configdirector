# Changelog

All notable changes to the ConfigDirector Terraform provider are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `configdirector_webhook` manages a webhook with Terraform or OpenTofu: its name, `https://` URL, custom `headers`, whether it is `enabled`, its project, environment and tag filters, and the `event_types` and `alert_kinds` it receives. A webhook that leaves out `event_types` receives every change event type, `event_types = []` receives none, and one that leaves out `alert_kinds` receives no alert events. The signing `secret` is a sensitive read-only attribute to pass to your receiver; no plan shows it, and it stays the same across updates. `headers` is sensitive too. Because ConfigDirector never returns header values, a header added or removed outside Terraform shows up in the next plan, but a value changed there does not. Import a webhook by its ID; the first apply after an import writes the header values the configuration declares. The API token needs `webhooks:manage`.
