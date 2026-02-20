# Changelog

All notable changes to Relay.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)

## [Unreleased]

### Changed
- README: restored mythology intro (Hermes the Messenger), character sigil and visual items, "Part of the Agora" section

## [2026-02-20]

### Added
- `relay metrics` command showing aggregate system stats: agent counts (alive/stale), total messages, reservations (active/expired), and commands (pending/total). Supports `--json` and `--stale` flags.

### Added
- Added user-level systemd service support with `deployment/relay.service` and `install-service.sh`.
- Added formal message schema docs (`docs/SCHEMA.md`) and schema validation/defaulting coverage for Relay messages.
- Added Go client coverage for `ReadOpts.From` filtering to support targeted dispatch/completion reads.

### Changed
- Documented user-level systemd service installation and operations in `README.md`.
- Expanded Go client notes in `README.md` with explicit agent resolution behavior.

### Fixed
- `relay spawn` now creates beads in the workspace database (`~/athena/workspace`) instead of target repos.
- `relay spawn` sets `DISPATCH_ENFORCE_PRD_LINT=false` when invoking dispatch for smoother orchestration.

## [2026-02-19]

### Added
- Added "For Agents" section in README: install, what-this-is, and runtime usage for agent consumers.
- Added `CHANGELOG.md` with Keep a Changelog format.
- Added `relay inbox` as an alias for `relay read` so inbox checks are easier to discover and script.
- Added `relay watch` with `fsnotify` to block until new inbox messages arrive, with `--loop` to keep watching after each delivery.

### Changed
- README mythology-forward rewrite to standardize voice across repos.

### Fixed
- `relay register` now rejects agent names that start with `-` (for example `--help`) to prevent flags from being accidentally stored as identities.
- Documented consistent agent-name resolution (`--agent`, then `RELAY_AGENT`, then hostname) in CLI help and README.
