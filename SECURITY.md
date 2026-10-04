# Security

[English](SECURITY.md) · [简体中文](SECURITY.zh-CN.md)

## Supported versions

Security fixes are applied to the latest released 0.x version. Earlier versions should be upgraded before reporting an issue.

## Reporting

Use GitHub's **Report a vulnerability** feature on this repository's Security tab for a private report. Include affected versions, reproduction steps, expected impact and a minimal example. Do not include real tokens, private page contents or someone else's personal data. If private reporting is unavailable, open an issue requesting a private contact without including exploit details.

## Deployment boundary

Pagehub is for trusted HTML on a trusted LAN. Public pages and the Dashboard have no login. Submitted JavaScript is executable, and all hosted pages currently share one origin; untrusted pages can access other public pages on that origin. Pagehub does not promise tenant or content isolation.

MCP management checks only whether the supplied Bearer token is active in the private local token registry or the legacy token file. Local and remote clients with that token can use every tool, including update and delete; source IP, Host, and Origin are not restricted. Use the endpoint on your trusted LAN. HTTP does not encrypt the token; public deployment requires an encrypted transport such as HTTPS. Send the token only in the Authorization header of the intended MCP endpoint, never in a page URL, query string, or HTML artifact. Client configuration files and the data directory are private local files. Only the explicit local CLI commands `token generate`, `token show`, and `token rotate` reveal credentials; the Dashboard, MCP tools, logs, and routine command output do not. Device tokens are independent, have identical permissions, and share one page store. Their names are labels, not verified device identities. Adding named tokens does not rewrite the original default token. Revoking or rotating one token leaves the others valid and rejects subsequent requests with that old token immediately, without restarting the service; already authorized in-flight operations may finish. A revoked token stays revoked across setup, restarts, and upgrades until explicitly generated or rotated.

Pagehub's doctor and acceptance clients send credentials only to their configured numeric loopback authority and MCP path. They disable HTTP redirects and environment proxies.

Background installation uses a macOS user LaunchAgent. It does not disable the firewall, request system-wide installation or run the service as root. Pagehub is distributed only as source; Homebrew and Go installations build the executable locally. No precompiled binaries or installers are published. Source installations make no Apple Developer ID signing or notarization claim.
