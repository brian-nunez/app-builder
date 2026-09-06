import jsonschema
from urllib.parse import quote, urlencode
from workflow_sdk import Integer, Json, Object, PluginError, Text, action

@action(
    name='community.openapi-operation',
    version='1.0.0',
    title='OpenAPI operation',
    description='Invoke an operation from a pinned OpenAPI document on a shared on-premises origin.',
    category='Services',
    config={
        'base_url': Text('API base URL', description='Service root prepended to the selected operation path.', default='https://services.internal'),
        'operation_id': Text('Operation ID', description='Exact operationId to execute from the OpenAPI document.', default=''),
        'document': Object('OpenAPI document', description='OpenAPI JSON document containing the selected operation.', default={}),
    },
    inputs={
        'parameters': Object('Parameters', required=False, sensitive=True),
        'body': Json('Body', required=False, sensitive=True),
        'token': Text('Token', required=False, sensitive=True),
    },
    outputs={
        'response': Json('Response', sensitive=True),
        'status': Integer('Status'),
    },
    permissions=['network.http'],
    log_fields=['operation_id', 'status'],
)
async def execute(ctx):
    document = ctx.config['document']
    found = []
    for path, item in document.get('paths', {}).items():
        for method, operation in item.items():
            if method in ('get', 'post', 'put', 'patch', 'delete', 'head', 'options') and operation.get('operationId') == ctx.config['operation_id']:
                found.append((path, method, item, operation))
    if len(found) != 1:
        raise PluginError('operation_not_found_or_ambiguous')
    path, method, item, operation = found[0]
    parameters = ctx.inputs.get('parameters', {})
    query = {}
    headers = {'Idempotency-Key': ctx.idempotency_key}
    if ctx.inputs.get('token'):
        headers['Authorization'] = 'Bearer ' + ctx.inputs['token']
    for parameter in item.get('parameters', []) + operation.get('parameters', []):
        name = parameter['name']
        if name not in parameters:
            if parameter.get('required'):
                raise PluginError('missing_parameter')
            continue
        value = parameters[name]
        jsonschema.validate(value, parameter.get('schema', {}))
        if parameter['in'] == 'path':
            path = path.replace('{' + name + '}', quote(str(value), safe=''))
        elif parameter['in'] == 'query':
            query[name] = value
        elif parameter['in'] == 'header':
            if name.lower() in ('host', 'authorization', 'content-length'):
                raise PluginError('reserved_header')
            headers[name] = str(value)
        else:
            raise PluginError('unsupported_parameter_location')
    if not path.startswith('/') or path.startswith('//') or '{' in path or '..' in path.split('/'):
        raise PluginError('invalid_operation_path')
    request_body = operation.get('requestBody', {})
    if request_body.get('required') and 'body' not in ctx.inputs:
        raise PluginError('missing_body')
    if 'body' in ctx.inputs:
        content = request_body.get('content', {}).get('application/json')
        if content is None:
            raise PluginError('unsupported_content_type')
        jsonschema.validate(ctx.inputs['body'], content.get('schema', {}))
    url = ctx.config['base_url'].rstrip('/') + path
    if query:
        url += '?' + urlencode(query, doseq=True)
    response = await ctx.http(method.upper(), url, headers=headers, json_body=ctx.inputs.get('body'))
    ctx.log('operation.completed', operation_id=ctx.config['operation_id'], status=response.status_code)
    return {'response': response.json() if response.content else None, 'status': response.status_code}
