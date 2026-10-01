#!/usr/bin/env python3
"""Register Pagehub in Codex, keeping the local token out of console output."""
import os
import re
import shutil
import subprocess
import tempfile
import tomllib
from pathlib import Path

home = Path.home()
token = (home / '.pagehub' / 'token').read_text().strip()
config = home / '.codex' / 'config.toml'
codex = shutil.which('codex')
if not codex:
    raise SystemExit('Codex CLI not found.')
subprocess.run([codex, 'mcp', 'add', 'pagehub', '--url', 'http://127.0.0.1:8765/_mcp'], check=True)
original = config.read_text()
before = tomllib.loads(original)
match = re.search(r'(?m)^\[mcp_servers\.pagehub\]\s*$', original)
if not match:
    raise SystemExit('Pagehub configuration section was not created.')
rest = original[match.end():]
next_table = re.search(r'(?m)^\[', rest)
end = match.end() + (next_table.start() if next_table else len(rest))
section = original[match.end():end]
section = re.sub(r'(?m)^http_headers\s*=.*\n?', '', section)
header = '\nhttp_headers = { Authorization = "Bearer ' + token + '" }\n'
modified = original[:match.end()] + header + section + original[end:]
after = tomllib.loads(modified)
assert after['mcp_servers']['pagehub']['http_headers']['Authorization'] == 'Bearer ' + token
after['mcp_servers']['pagehub'].pop('http_headers')
before['mcp_servers']['pagehub'].pop('http_headers', None)
assert before == after, 'Unexpected unrelated configuration change'
fd, staged = tempfile.mkstemp(prefix='.pagehub-config-', dir=config.parent)
try:
    with os.fdopen(fd, 'w') as f:
        f.write(modified)
        f.flush()
        os.fsync(f.fileno())
    os.chmod(staged, 0o600)
    os.replace(staged, config)
finally:
    if os.path.exists(staged):
        os.unlink(staged)
subprocess.run([codex, 'mcp', 'get', 'pagehub'], check=True)
print('Pagehub registered with a private local authorization header.')
