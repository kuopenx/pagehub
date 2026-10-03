# Releasing Pagehub

[English](releasing.md) · [简体中文](releasing.zh-CN.md)

Pagehub is distributed only as source. Supported installation paths are Homebrew, `go install`, and a local source build. Never upload precompiled executables, installer packages, or binary archive/checksum attachments to a Release. Apple signing and notarization are not part of this release process.

## Prerequisites

Maintainers need push and Release permissions. The Homebrew tap is [kuopenx/homebrew-tap](https://github.com/kuopenx/homebrew-tap); its formula builds a versioned source archive with Go. Pagehub has no self-updater.

## Validate and publish

1. Update `internal/buildinfo.Version`, the README version, and documentation in both English and Simplified Chinese. Add matching `docs/releases/vX.Y.Z.md` and `docs/releases/vX.Y.Z.zh-CN.md` release notes; the source-release workflow uses the tagged English file as its Release body.
2. Run the checks and real disposable acceptance in [CONTRIBUTING.md](../CONTRIBUTING.md). For workflow changes, also validate YAML and embedded shell syntax.
3. Refresh `THIRD_PARTY_NOTICES.md` if dependencies changed. Review the diff for secrets and generated files.
4. Push the commit and wait for macOS/Linux CI to succeed.
5. Push an annotated `vX.Y.Z` tag pointing to that tested commit. The **Source release** workflow checks the tagged source, runs real acceptance, and creates a draft with installation instructions. It uploads no assets. Maintainers can also dispatch it manually with an existing tag.
6. Inspect the draft and confirm its asset list is empty. Publish it after the workflow succeeds. GitHub automatically provides source ZIP and tar.gz links.
7. Run the tap's **Update Pagehub formula** workflow, or wait for its daily schedule. It reads the latest published source release and updates the tag URL, source SHA256, and embedded commit. Verify source installation and `brew test kuopenx/tap/pagehub`.
8. Verify `go install github.com/kuopenx/pagehub/cmd/pagehub@vX.Y.Z` in a temporary `GOBIN`, and check the installed version. The README's `@latest` follows the highest published Go module version tag; do not leave a failed release tag in place.

Never move an existing published tag. Software versions, page revisions, and settings-schema versions are separate concepts. Source archives have no uploaded executable. Local builds report the source version; Homebrew also embeds the tagged commit.

## Existing release cleanup

Remove any previously uploaded binary archives and binary `checksums.txt` attachments, including draft releases. Retain source version tags and update published release notes to describe source installation. GitHub's automatic source archives are expected and are not binary packages. The repository must not commit executables; do not rewrite source history merely to remove binaries that were never committed.

## Upgrade compatibility

After installing a newer version from source, run `pagehub setup` on macOS or Linux to update the managed background executable. Restart foreground instances separately. Homebrew's executable is never overwritten by Pagehub. Setup preserves pages, token, settings, and revisions, waits for health, and restores the prior executable/service configuration when startup fails. The legacy 0.2.0 launchd label migrates to `io.pagehub.agent` after successful startup.

Setup also migrates a saved custom service label when `--service-name` changes. Readiness requires a Pagehub health response from the PID reported by launchd or systemd; an unrelated HTTP 200 response or redirect does not qualify. Local management clients pin the loopback authority and never follow redirects or use environment proxies.

Settings currently use schema version 1. Existing page metadata without a revision is read as revision 1. Future page-format changes need fixtures, restart tests, and a rollback strategy before release.
