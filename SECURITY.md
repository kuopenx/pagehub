# Security

## Supported versions

Security fixes are applied to the latest released 0.x version. Earlier versions should be upgraded before reporting an issue.

## Reporting

Use GitHub's **Report a vulnerability** feature on this repository's Security tab for a private report. Include affected versions, reproduction steps, expected impact and a minimal example. Do not include real tokens, private page contents or someone else's personal data. If private reporting is unavailable, open an issue requesting a private contact without including exploit details.

## Deployment boundary

Pagehub is for trusted HTML on a trusted LAN. Public pages and the Dashboard have no login. Submitted JavaScript is executable, and all hosted pages currently share one origin; untrusted pages can access other public pages on that origin. Pagehub does not promise tenant or content isolation.

MCP management requires a persistent Bearer token, loopback requests and valid Host/Origin. Never send that token to a LAN URL, expose the MCP endpoint through a public proxy, or place the token in an HTML artifact. Client configuration files and the data directory are private local files.

Background installation uses a macOS user LaunchAgent. It does not disable the firewall, request system-wide installation or run the service as root. macOS distribution archives are not Developer ID signed or notarized unless the corresponding release process explicitly states otherwise.
