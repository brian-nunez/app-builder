from workflow_sdk import Json, resource


@resource(
    name='community.static-config',
    version='1.0.0',
    title='Static configuration',
    description='Expose a reusable JSON value. Use a secret reference for credentials.',
    category='Configuration',
    config={
        'value': Json('Value', description='JSON value supplied to connected inputs.', default={}),
    },
    outputs={
        'value': Json('Value'),
    },
)
async def execute(ctx):
    return {'value': ctx.config['value']}
