#!/usr/bin/env python3
"""Register Pagehub globally in Claude Code without exposing its token."""
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

home = Path.home()
config = home / '.claude.json'
token = (home / '.pagehub' / 'token').read_text().strip()
claude = shutil.which('claude')
if not claude:
    raise SystemExit('Claude CLI not found.')
initial = json.loads(config.read_text()) if config.exists() else {}
existing = initial.get('mcpServers', {}).get('pagehub')
url = 'http://127.0.0.1:8765/_mcp'
if existing and (existing.get('type') != 'http' or existing.get('url') != url):
    raise SystemExit('An unrelated user-level MCP named pagehub already exists; leaving it unchanged.')
if not existing:
    result = subprocess.run(
        [claude, 'mcp', 'add', '--scope', 'user', '--transport', 'http', 'pagehub', url],
        capture_output=True, text=True,
    )
    if result.returncode:
        raise SystemExit((result.stdout + result.stderr).replace(token, '[redacted]'))
original = config.read_text()
before = json.loads(original)
modified = json.loads(original)
server = modified['mcpServers']['pagehub']
assert server['type'] == 'http' and server['url'] == url
server.setdefault('headers', {})['Authorization'] = 'Bearer ' + token
compare = json.loads(json.dumps(modified))
compare['mcpServers']['pagehub'] = before['mcpServers']['pagehub']
assert compare == before, 'Unexpected unrelated configuration change'
fd, staged = tempfile.mkstemp(prefix='.pagehub-claude-', dir=config.parent)
try:
    with os.fdopen(fd, 'w') as f:
        json.dump(modified, f, ensure_ascii=False, indent=2)
        f.write('\n')
        f.flush()
        os.fsync(f.fileno())
    os.chmod(staged, 0o600)
    if config.read_text() != original:
        raise SystemExit('Claude configuration changed concurrently; run the installer again.')
    os.replace(staged, config)
finally:
    if os.path.exists(staged):
        os.unlink(staged)
print('Registered pagehub in Claude Code user scope with its existing private token.')
# get performs Claude's own MCP connection/health check. Redact any header output.
result = subprocess.run([claude, 'mcp', 'get', 'pagehub'], capture_output=True, text=True, timeout=45)
print((result.stdout + result.stderr).replace(token, '[redacted]'), end='')
if result.returncode:
    raise SystemExit(result.returncode)
