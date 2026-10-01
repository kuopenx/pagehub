#!/usr/bin/env python3
"""Install the locally built server and its user-level LaunchAgent."""
import os
import plistlib
import shutil
import subprocess
from pathlib import Path

project = Path(__file__).resolve().parent
binary = project / 'pagehub'
if not binary.is_file():
    raise SystemExit('Build pagehub before installing.')
home = Path.home()
data = home / '.pagehub'
data.mkdir(mode=0o700, exist_ok=True)
bin_dir = data / 'bin'
bin_dir.mkdir(mode=0o700, exist_ok=True)
target = bin_dir / 'pagehub'
staged = bin_dir / '.pagehub-new'
shutil.copyfile(binary, staged)
staged.chmod(0o700)
staged.replace(target)

label = 'com.garyshu.pagehub'
plist_path = home / 'Library' / 'LaunchAgents' / (label + '.plist')
plist_path.parent.mkdir(parents=True, exist_ok=True)
plist = {
    'Label': label,
    'ProgramArguments': [str(target), '--data-dir', str(data), '--port', '8765'],
    'RunAtLoad': True,
    'KeepAlive': True,
    'ThrottleInterval': 10,
    'ProcessType': 'Background',
    'StandardOutPath': '/dev/null',
    'StandardErrorPath': '/dev/null',
}
plist_path.write_bytes(plistlib.dumps(plist))
plist_path.chmod(0o600)
domain = f'gui/{os.getuid()}'
service = f'{domain}/{label}'
if subprocess.run(['launchctl', 'print', service], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
    subprocess.run(['launchctl', 'bootout', service], check=True)
subprocess.run(['launchctl', 'enable', service], check=True)
subprocess.run(['launchctl', 'bootstrap', domain, str(plist_path)], check=True)
print('Installed:', target)
print('LaunchAgent:', plist_path)
print('Dashboard: http://127.0.0.1:8765/')
