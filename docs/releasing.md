# Releasing Pagehub

## Prerequisites

The release maintainer needs push and Releases permissions. The Homebrew tap is `kuopenx/homebrew-tap`; it contains a source-build formula, so normal Homebrew upgrades do not need a self-updater inside Pagehub.

## Checks and publishing

1. Update `internal/buildinfo.Version`, release notes and relevant documentation.
2. Run the CI checks and real disposable acceptance from [CONTRIBUTING.md](../CONTRIBUTING.md).
3. Refresh `THIRD_PARTY_NOTICES.md` if dependencies changed.
4. Validate packaging with `goreleaser check` and `goreleaser release --snapshot --clean`.
5. Push an annotated `vX.Y.Z` tag. The Release workflow creates a **draft** with macOS/Linux arm64/amd64 archives and `checksums.txt`.
6. Download and test the macOS archive. Optionally run the macOS signing workflow while the release is still a draft. Signing requires your own Apple Developer ID certificate and notarization credentials.
7. Publish the draft after checking its assets. Update the tap formula's tag URL and source SHA256; run `brew install --build-from-source kuopenx/tap/pagehub` and `brew test kuopenx/tap/pagehub`.

Release tags must refer to commits that passed CI. Never modify an existing published tag. Version, Git commit and build date are embedded in release binaries. Software versions, per-page revisions and settings schema versions serve different purposes.

## Signing and notarization

[macos-sign.yml](../.github/workflows/macos-sign.yml) is an optional manual workflow for a draft release. Set these repository secrets:

- `APPLE_CERTIFICATE_P12`: base64 encoded Developer ID Application certificate including private key.
- `APPLE_CERTIFICATE_PASSWORD`: certificate password.
- `APPLE_SIGNING_IDENTITY`: Developer ID Application signing identity.
- `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD`: notarization credentials.

The workflow signs both macOS binaries with hardened runtime, submits ZIPs for notarization, checks the response, replaces the draft macOS assets and updates checksums. Standalone CLI binaries cannot be stapled like app bundles; notarization tickets are checked online. Do not claim a release is notarized unless that workflow succeeded.

No valid signing identity is assumed on a developer machine. Unsigned releases are explicitly marked as such. Source builds and Homebrew installation are available without uploading Apple credentials.

## Upgrade compatibility

Run the newer binary's `setup` command to update the managed background executable. Homebrew's binary is never overwritten by Pagehub. Setup preserves pages, token, settings and revisions, waits for health, and restores the prior binary/service configuration when startup fails. The legacy 0.2.0 launchd label is migrated to `io.pagehub.agent` after successful startup.

Settings currently use schema version 1. Existing page metadata without a revision is read as revision 1. Any future page-format migration needs fixtures, restart tests and a rollback strategy before release.
