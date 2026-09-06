from urllib.parse import quote

from workflow_sdk import Boolean, Json, Text, resource


@resource(
    name='community.consul-kv',
    version='1.0.0',
    title='Consul key/value',
    description='Resolve a configuration key from an explicitly configured Consul service.',
    category='Configuration',
    config={
        'url': Text('Consul URL', description='Base URL of the Consul HTTP API.', default='https://consul.internal'),
        'key': Text('Key path', description='Consul KV path to read.', default='services/app/config'),
        'parse_json': Boolean('Parse JSON', description='Decode the stored value as JSON instead of returning text.', default=False),
    },
    inputs={
        'token': Text('Token', required=False, sensitive=True),
    },
    outputs={
        'value': Json('Value', sensitive=True),
    },
    permissions=['network.http'],
)
async def execute(ctx):
    headers = {'X-Consul-Token': ctx.inputs['token']} if ctx.inputs.get('token') else {}
    response = await ctx.http('GET', ctx.config['url'].rstrip('/') + '/v1/kv/' + quote(ctx.config['key'], safe='/') + '?raw', headers=headers)
    return {'value': response.json() if ctx.config['parse_json'] else response.text}
