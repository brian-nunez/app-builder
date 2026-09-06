import base64
import json
import os
import secrets
import subprocess
from pathlib import Path


def tailscale_hostname():
    candidates = [
        ['tailscale', 'status', '--json'],
        ['/Applications/Tailscale.app/Contents/MacOS/Tailscale', 'status', '--json'],
    ]
    for command in candidates:
        try:
            result = subprocess.run(command, check=True, capture_output=True, text=True)
            hostname = json.loads(result.stdout).get('Self', {}).get('DNSName', '').rstrip('.')
            if hostname:
                return hostname
        except (FileNotFoundError, subprocess.CalledProcessError, json.JSONDecodeError):
            continue
    return 'host.docker.internal'


path = Path('.env')
existing = {}
if path.exists():
    for line in path.read_text().splitlines():
        key, separator, value = line.partition('=')
        if separator:
            existing[key] = value

content = {
    'PLATFORM_DB_PASSWORD': lambda: secrets.token_urlsafe(32),
    'PLATFORM_ACCESS_TOKEN': lambda: secrets.token_urlsafe(32),
    'PLATFORM_ENCRYPTION_KEY': lambda: base64.b64encode(os.urandom(32)).decode(),
    'WORKER_TOKEN': lambda: secrets.token_urlsafe(32),
    'PLUGIN_ALLOWED_HOSTS': lambda: '',
    'APP_PUBLIC_HOST': tailscale_hostname,
    'KEYCLOAK_DB_PASSWORD': lambda: secrets.token_urlsafe(32),
    'KEYCLOAK_ADMIN_PASSWORD': lambda: secrets.token_urlsafe(24),
    'KEYCLOAK_CLIENT_SECRET': lambda: secrets.token_urlsafe(32),
    'KEYCLOAK_BRIAN_PASSWORD': lambda: secrets.token_urlsafe(18),
}
missing = {key: generate() for key, generate in content.items() if key not in existing}
if missing:
    flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND
    if not path.exists():
        flags |= os.O_EXCL
    fd = os.open(path, flags, 0o600)
    with os.fdopen(fd, 'a') as file:
        file.write(''.join(key + '=' + value + '\n' for key, value in missing.items()))
    os.chmod(path, 0o600)
    print('Added missing local configuration with unique credentials (mode 0600).')
else:
    print('Existing local configuration retained.')
