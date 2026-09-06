from urllib.parse import quote

from workflow_sdk import Json, Text, resource


@resource(
    name='community.vault-kv',
    version='1.0.0',
    title='Vault KV',
    description='Read one field from a HashiCorp Vault KV v2 secret.',
    category='Configuration',
    config={
        'url': Text('Vault URL', description='Base URL of the Vault HTTP API.', default='https://vault.internal'),
        'mount': Text('KV mount', description='Name of the KV v2 secrets engine mount.', default='secret'),
        'path': Text('Secret path', description='Path to the secret within the mount.', default='services/app'),
        'field': Text('Secret field', description='Field to return from the Vault secret.', default='password'),
    },
    inputs={
        'token': Text('Token', sensitive=True),
    },
    outputs={
        'value': Json('Value', sensitive=True),
    },
    permissions=['network.http'],
)
async def execute(ctx):
    c = ctx.config
    url = c['url'].rstrip('/') + '/v1/' + quote(c['mount'], safe='') + '/data/' + quote(c['path'], safe='/')
    response = await ctx.http('GET', url, headers={'X-Vault-Token': ctx.inputs['token']})
    return {'value': response.json()['data']['data'][c['field']]}
