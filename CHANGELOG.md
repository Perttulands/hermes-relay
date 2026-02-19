# Changelog

All notable changes to Relay.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)

## [Unreleased]

### Changed
- README: mythology-forward rewrite — each README now reads like discovering a character in a world

### Added
- "For Agents" section in README: install, what-this-is, and runtime usage for agent consumers
- CHANGELOG.md with Keep a Changelog format

### Fixed
- 2026-02-19: `relay register` now rejects agent names that start with `-` (for example `--help`) to prevent flags from being accidentally stored as identities.
