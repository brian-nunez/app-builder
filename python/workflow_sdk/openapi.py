import json
import re
from copy import deepcopy
from pathlib import Path
from .registry import PROTOCOL, digest


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
    if not re.fullmatch(r'[a-z][a-z0-9._-]{1,100}', name):
        raise ValueError('Invalid plugin name')
    document = json.loads(Path(path).read_text())
    if not str(document.get('openapi', '')).startswith('3.'):
        raise ValueError('OpenAPI 3.x JSON is required')
    document = resolve_local(document, document)
    matches = [(path, method, item, operation) for path, item in document.get('paths', {}).items() for method, operation in item.items() if isinstance(operation, dict) and operation.get('operationId') == operation_id]
    if len(matches) != 1:
        raise ValueError('Operation ID must identify exactly one operation')
    path, method, item, operation = matches[0]
    parameters = { (p['in'],p['name']):p for p in item.get('parameters', []) + operation.get('parameters', []) }
    inputs = []
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
        inputs.append({'name': port, 'title': parameter['name'], 'kind': 'data', 'required': parameter.get('required', False), 'sensitive': True, 'schema': parameter.get('schema', {})})
    request = operation.get('requestBody', {})
    if request:
        content = request.get('content', {}).get('application/json')
        if content is None:
            raise ValueError('Import requires JSON request bodies')
        inputs.append({'name': 'body', 'title': 'Body', 'kind': 'data', 'sensitive': True, 'required': request.get('required', False), 'schema': content.get('schema', {})})
    inputs.append({'name': 'token', 'title': 'Bearer token', 'kind': 'data', 'sensitive': True, 'schema': {'type': 'string'}})
    manifest = {'protocol': PROTOCOL, 'name': name, 'version': '1.0.0', 'title': operation.get('summary', operation_id), 'description': operation.get('description', 'An imported OpenAPI operation.'), 'category': 'Services', 'kind': 'action', 'configSchema': {'type':'object','properties':{'base_url':{'type':'string','default':base_url}},'required':['base_url'],'additionalProperties':False}, 'inputs': inputs, 'outputs': [{'name': 'response','title':'Response','kind':'data','required':True,'sensitive':True,'schema':{}},{'name':'status','title':'HTTP status','kind':'data','required':True,'schema':{'type':'integer'}}], 'permissions':['network.http'],'logFields':['operation_id','status'],'metrics':[],'digest':''}
    source = Path(__file__).with_name('openapi_handler.py').read_text()
    document = {'openapi': document['openapi'], 'paths': {path: {method: deepcopy(operation), 'parameters': item.get('parameters', [])}}}
    source += '\nDOCUMENT = ' + repr(document) + '\nOPERATION_ID = ' + repr(operation_id) + '\nMAPPING = ' + repr(mapping) + '\n'
    directory = Path(root) / name
    directory.mkdir(parents=True, exist_ok=False)
    (directory/'plugin.py').write_text(source)
    (directory/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    manifest['digest'] = digest(directory)
    (directory/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    return directory
