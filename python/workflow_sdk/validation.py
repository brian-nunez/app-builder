import json
from pathlib import Path

import jsonschema

from .declare import IDENTIFIER, KINDS, VERSION

SIDES = ('left', 'right', 'top', 'bottom')
PLACEHOLDER_DIGEST = '0' * 64
_specification = None


def specification():
    global _specification
    if _specification is None:
        _specification = json.loads(Path(__file__).with_name('manifest.schema.json').read_text())
    return _specification


def validate_manifest(manifest, *, require_digest=True):
    """Structural checks the JSON Schema cannot express on its own."""
    subject = manifest
    if not require_digest:
        subject = {**manifest, 'digest': PLACEHOLDER_DIGEST}
    jsonschema.validate(subject, specification())
    if not IDENTIFIER.fullmatch(manifest['name']) or not VERSION.fullmatch(manifest['version']):
        raise ValueError('Invalid plugin identity')
    if manifest['kind'] not in KINDS:
        raise ValueError('Invalid plugin kind')
    schemas = [manifest['configSchema']]
    for ports in (manifest['inputs'], manifest['outputs']):
        seen = set()
        for port in ports:
            if not IDENTIFIER.fullmatch(port['name']) or port['name'] in seen:
                raise ValueError('Invalid or duplicate port name')
            seen.add(port['name'])
            if port['kind'] not in ('data', 'resource') or (port['kind'] == 'resource' and not port.get('resourceType')):
                raise ValueError('Invalid port kind or resource type')
            if port.get('side') is not None and port['side'] not in SIDES:
                raise ValueError('Invalid port side')
            schemas.append(port['schema'])
    for schema in schemas:
        if not isinstance(schema, dict):
            raise ValueError('Schemas must be JSON objects')
        jsonschema.Draft202012Validator.check_schema(schema)
    required = manifest['configSchema'].get('required', [])
    properties = manifest['configSchema'].get('properties', {})
    for name in required:
        if name not in properties:
            raise ValueError(f'Required configuration property {name!r} is not declared')
    metrics = set()
    for metric in manifest.get('metrics', []):
        if metric['kind'] not in ('counter', 'histogram') or not IDENTIFIER.fullmatch(metric['name']) or metric['name'] in metrics:
            raise ValueError('Invalid metric declaration')
        metrics.add(metric['name'])
    fields = manifest.get('logFields', [])
    if len(set(fields)) != len(fields):
        raise ValueError('Duplicate diagnostic fields')
    for field in fields:
        if not IDENTIFIER.fullmatch(field):
            raise ValueError('Invalid diagnostic field')
    return manifest


__all__ = ['validate_manifest', 'specification', 'SIDES']
