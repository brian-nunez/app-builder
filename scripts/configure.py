import base64
import os
import secrets
from pathlib import Path

path = Path('.env')
if path.exists():
    print('Existing local configuration retained.')
else:
    content = {
        'PLATFORM_DB_PASSWORD': secrets.token_urlsafe(32),
        'PLATFORM_ACCESS_TOKEN': secrets.token_urlsafe(32),
        'PLATFORM_ENCRYPTION_KEY': base64.b64encode(os.urandom(32)).decode(),
        'WORKER_TOKEN': secrets.token_urlsafe(32),
        'PLUGIN_ALLOWED_HOSTS': '',
    }
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as file:
        file.write(''.join(key + '=' + value + '\n' for key, value in content.items()))
    print('Created .env with unique local credentials (mode 0600).')
