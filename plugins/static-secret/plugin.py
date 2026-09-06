from workflow_sdk import Text, resource


@resource(
    name='community.static-secret',
    version='1.0.0',
    title='Static credential',
    description='Supply a credential stored in encrypted node configuration. Prefer Vault for managed rotation.',
    category='Configuration',
    config={
        'value': Text(
            'Secret value',
            description='Credential stored encrypted and supplied only to sensitive inputs.',
            default='',
            secret=True,
        ),
    },
    outputs={
        'value': Text('Credential', sensitive=True),
    },
)
async def execute(ctx):
    return {'value': ctx.config['value']}
