import re
import json
from pathlib import Path
import jsonschema


def validate_manifest(manifest):
    specification = json.loads(Path(__file__).with_name('manifest.schema.json').read_text())
    jsonschema.validate(manifest, specification)
    identity = re.compile(r'^[a-z][a-z0-9._-]{0,100}$')
    if not identity.fullmatch(manifest['name']) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', manifest['version']):
        raise ValueError('Invalid plugin identity')
    if manifest['kind'] not in ('action', 'resource', 'trigger'):
        raise ValueError('Invalid plugin kind')
    schemas = [manifest['configSchema']]
    for ports in (manifest['inputs'], manifest['outputs']):
        seen = set()
        for port in ports:
            if not identity.fullmatch(port['name']) or port['name'] in seen:
                raise ValueError('Invalid or duplicate port name')
            seen.add(port['name'])
            if port['kind'] not in ('data', 'resource') or (port['kind'] == 'resource' and not port.get('resourceType')):
                raise ValueError('Invalid port kind or resource type')
            schemas.append(port['schema'])
    for schema in schemas:
        if not isinstance(schema, dict):
            raise ValueError('Schemas must be JSON objects')
        jsonschema.Draft202012Validator.check_schema(schema)
    metrics = set()
    for metric in manifest.get('metrics', []):
        if metric['kind'] not in ('counter', 'histogram') or not identity.fullmatch(metric['name']) or metric['name'] in metrics:
            raise ValueError('Invalid metric declaration')
        metrics.add(metric['name'])
    if len(set(manifest.get('logFields', []))) != len(manifest.get('logFields', [])):
        raise ValueError('Duplicate diagnostic fields')
