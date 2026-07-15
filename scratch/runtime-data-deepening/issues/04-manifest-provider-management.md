# Make provider build automatically create a managed installation

Status: complete

Completed: 2026-07-14

## Completion evidence

- `internal/app/providers.BuildAndInstall` builds a staged manifest package,
  validates it, and atomically activates it as official or custom provenance.
- `plugin build` automatically installs; its CLI tracer immediately observes
  the custom provider through the real runtime registry.
- Failed builds preserve the active installation, custom installs retain
  reserved-name enforcement, and build-all continues to report independent
  failures.
- Plugin list/status/install now use provider-management application behavior.
  Filename discovery, prompts, fallback catalogs, unmanaged outputs, and the
  complete `pkg/plugins` module were deleted.
- Managed official installations take precedence over local duplicates, while local-only official builds remain runtime-visible.

Type: AFK

## Parent

`scratch/runtime-data-deepening/PRD.md`

## What to build

Make provider build, packaging, installation, list, and status one manifest-driven workflow. A successful `plugin build` must package the executable with its manifest, install it into the managed provider directory, and leave it immediately visible to the real runtime registry.

Official-provider build/install uses official provenance. Custom manifest installation retains reserved-name enforcement. Once all command paths use the managed workflow, delete filename-only discovery, duplicate catalogs, unmanaged output paths, and obsolete compatibility interfaces.

## Problem Statement

Legacy plugin management still searches for executable filenames across multiple directories and maintains separate provider metadata. The ordinary build path can create a binary that is not a valid managed installation and is invisible to the manifest-driven runtime. Build, install, list, and status can therefore disagree.

## Solution

Deepen provider management around source build, manifest packaging, managed activation, runtime registration, and status. Keep process invocation as an internal build adapter. Make automatic managed installation the only successful build outcome, then delete the alternate discovery and catalog implementations.

## Acceptance criteria

- [x] Successful `plugin build <provider>` produces a validated manifest package and installs it automatically.
- [x] The installed provider is immediately listed by the real runtime with correct origin, version, capabilities, and storage registration.
- [x] Official builds install with official provenance and cannot be impersonated by a custom manifest.
- [x] Custom manifest installs continue to reject reserved official names.
- [x] Build failure leaves the prior managed installation unchanged.
- [x] Packaging or installation failure returns a nonzero command outcome and leaves no unmanaged active binary.
- [x] Plugin list and status read exclusively from the manifest-driven runtime registry.
- [x] Filename-only search paths, duplicate registry metadata, hard-coded fallback catalogs, prompts, and obsolete plugin-manager interfaces are deleted.
- [x] No production catalog hard-codes custom provider names.
- [x] Architecture tests reject reintroduction of filename-only discovery.

## Commits

1. Add a CLI-level tracer test proving a locally built fixture provider becomes a runtime-visible managed installation without a separate install command.
2. Add behavioral tests for official provenance, custom reserved-name rejection, replacement, and failed-build preservation.
3. Isolate source build execution as an internal provider-management adapter with captured output and explicit artifact metadata.
4. Make a successful build produce a staged executable plus the provider's versioned manifest.
5. Validate the staged package through the same manifest rules used by runtime discovery.
6. Add managed official-provider activation that records official provenance without weakening custom reserved-name checks.
7. Route custom manifest packages through the existing managed custom-provider activation behavior.
8. Change `plugin build` to build, package, and automatically install before reporting success.
9. Make plugin list and status query only the real provider runtime registry.
10. Make plugin install accept manifest packages only and remove interactive build prompting.
11. Migrate build-all behavior so each provider reports an independently valid managed installation result.
12. Delete filename-derived binary lookup and legacy search directories.
13. Delete the duplicate provider registry metadata and hard-coded provider fallback catalog.
14. Delete unmanaged binary output paths and unused provider-specific build compatibility code after the managed build adapter covers shipped providers.
15. Delete the legacy plugin-manager module once no caller remains.
16. Add architecture guardrails that require provider discovery and status to flow through manifests and the runtime registry.
17. Run provider, CLI, hermetic integration, architecture, and full repository tests.

## Decision Document

- `plugin build` automatically installs on success.
- A build is not successful unless the runtime can discover the managed installation.
- Breaking deletion of legacy plugin-management interfaces is explicitly allowed.
- Official and custom installations share packaging and validation but retain distinct provenance and reserved-name rules.
- Runtime registry state is authoritative for installed status.
- Provider source availability is build metadata, not installed-provider status.
- No filename-only executable discovery remains.
- Managed official provenance wins duplicate resolution over the development build root.

## Testing Decisions

- Tests use local fixture source, manifests, temporary HOME directories, and the real managed installation path.
- No test invokes cloud tools, requires credentials, or reaches the network.
- Tests assert runtime-visible behavior rather than build-command call order.
- Replacement and failure tests assert the previously active installation remains usable.
- Existing managed custom-provider and CLI fixture tests provide prior art.

## Blocked by

None - can start immediately. Coordinate architecture-guardrail edits with issue 01 if implemented concurrently.

## Out of Scope

- Downloading providers from a remote marketplace.
- Signing or trust-store policy.
- Provider protocol changes.
- Crash-safe concurrent managed activation beyond preserving the current replacement contract.
