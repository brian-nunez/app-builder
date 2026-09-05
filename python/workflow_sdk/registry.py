import hashlib
import json
from pathlib import Path
from .validation import validate_manifest

PROTOCOL = 'workflow.plugin/v1'


def digest(directory):
    h = hashlib.sha256()
    for path in sorted(directory.rglob('*')):
        if not path.is_file() or '__pycache__' in path.parts or path.name == 'manifest.json':
            continue
        h.update(path.relative_to(directory).as_posix().encode() + b'\0')
        h.update(path.read_bytes())
    manifest = json.loads((directory / 'manifest.json').read_text())
    manifest.pop('digest', None)
    h.update(json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode())
    return h.hexdigest()


def catalog(root):
    result = {}
    for path in sorted(Path(root).glob('*/manifest.json')):
        manifest = json.loads(path.read_text())
        if manifest['protocol'] != PROTOCOL:
            raise ValueError('Invalid plugin artifact: ' + str(path.parent))
        validate_manifest(manifest)
        key = (manifest['name'], manifest['version'])
        if key in result:
            raise ValueError('Duplicate plugin version')
        result[key] = (manifest, path.parent.resolve())
    return result
