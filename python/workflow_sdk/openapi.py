import json
import re
from copy import deepcopy
from pathlib import Path

from .declare import IDENTIFIER
from .registry import build

METHODS = ('get', 'post', 'put', 'patch', 'delete', 'head', 'options')


def resolve_local(document, value, stack=()):
    if isinstance(value, list):
        return [resolve_local(document, item, stack) for item in value]
    if not isinstance(value, dict):
        return value
    if '$ref' in value:
        reference = value['$ref']
        if not reference.startswith('#/') or reference in stack:
            raise ValueError('Only acyclic local OpenAPI references can be imported')
        target = document
        for part in reference[2:].split('/'):
            target = target[part.replace('~1', '/').replace('~0', '~')]
        return resolve_local(document, target, stack + (reference,))
    result = {key: resolve_local(document, item, stack) for key, item in value.items() if key != 'nullable'}
    if value.get('nullable') and isinstance(result.get('type'), str):
        result['type'] = [result['type'], 'null']
    return result


def import_operation(path, operation_id, name, base_url, root):
    if not IDENTIFIER.fullmatch(name) or '.' not in name:
        raise ValueError('Invalid plugin name')
    document = json.loads(Path(path).read_text())
    if not str(document.get('openapi', '')).startswith('3.'):
        raise ValueError('OpenAPI 3.x JSON is required')
    document = resolve_local(document, document)
    matches = [
        (route, method, item, operation)
        for route, item in document.get('paths', {}).items()
        for method, operation in item.items()
        if isinstance(operation, dict) and operation.get('operationId') == operation_id
    ]
    if len(matches) != 1:
        raise ValueError('Operation ID must identify exactly one operation')
    route, method, item, operation = matches[0]
    parameters = {(p['in'], p['name']): p for p in item.get('parameters', []) + operation.get('parameters', [])}
    ports = {}
    mapping = {}
    parameter_names = set()
    for parameter in parameters.values():
        if parameter['name'] in parameter_names:
            raise ValueError('Duplicate parameter names across locations are not supported by this importer')
        parameter_names.add(parameter['name'])
        port = re.sub(r'[^a-z0-9._-]', '_', parameter['in'] + '_' + parameter['name'].lower())
        if port in mapping:
            raise ValueError('Parameter names collide after normalization')
        if parameter.get('style') not in (None, 'form', 'simple') or parameter.get('schema', {}).get('type') in ('array', 'object'):
            raise ValueError('Import currently supports scalar path, query, and header parameters')
        mapping[port] = parameter['name']
        ports[port] = {
            'schema': parameter.get('schema', {}),
            'title': parameter['name'],
            'required': bool(parameter.get('required', False)),
        }
    request = operation.get('requestBody', {})
    if request:
        content = request.get('content', {}).get('application/json')
        if content is None:
            raise ValueError('Import requires JSON request bodies')
        ports['body'] = {
            'schema': content.get('schema', {}),
            'title': 'Body',
            'required': bool(request.get('required', False)),
        }
    ports['token'] = {'schema': {'type': 'string'}, 'title': 'Bearer token', 'required': False}

    pinned = {'openapi': document['openapi'], 'paths': {route: {method: deepcopy(operation), 'parameters': item.get('parameters', [])}}}
    inputs = ',\n'.join(
        f'    {port!r}: Raw({spec["schema"]!r}, {spec["title"]!r}, required={spec["required"]!r}, sensitive=True)'
        for port, spec in ports.items()
    )
    source = Path(__file__).with_name('openapi_handler.py').read_text()
    source += f'''

DOCUMENT = {pinned!r}
OPERATION_ID = {operation_id!r}
MAPPING = {mapping!r}

INPUTS = {{
{inputs},
}}

execute = action(
    name={name!r},
    version='1.0.0',
    title={operation.get('summary', operation_id)!r},
    description={operation.get('description', 'An imported OpenAPI operation.')!r},
    category='Services',
    config={{'base_url': Text('API base URL', description='Service root prepended to the operation path.', default={base_url!r})}},
    inputs=INPUTS,
    outputs={{
        'response': Json('Response', sensitive=True),
        'status': Integer('HTTP status'),
    }},
    permissions=['network.http'],
    log_fields=['operation_id', 'status'],
)(execute)
'''
    directory = Path(root) / name
    directory.mkdir(parents=True, exist_ok=False)
    (directory / 'plugin.py').write_text(source)
    build(directory)
    return directory
