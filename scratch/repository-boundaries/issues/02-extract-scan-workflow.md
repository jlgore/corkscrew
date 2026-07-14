# Extract the scan application workflow

Status: ready-for-human

- [x] Move service-group ownership out of the CLI package.
- [x] Move service and region normalization out of the command handler.
- [x] Move Quack URL and token precedence out of the command handler.
- [x] Inject execution dependencies for tests without loading a plugin.
- [x] Keep the CLI adapter responsible only for flags and request construction.
- [x] Demonstrate that custom provider names pass through orchestration.

## Comments

Implemented for review on 2026-07-14.
