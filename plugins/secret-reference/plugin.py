from workflow_sdk import Text, resource


@resource(
    name='community.secret-reference',
    version='1.0.0',
    title='Environment secret',
    description='Resolve an operator-provisioned PLUGIN_SECRET_ variable without saving its value.',
    category='Configuration',
    config={
        'variable': Text(
            'Environment variable',
            description='Name of an allowlisted variable available to the worker.',
            pattern='^PLUGIN_SECRET_[A-Z0-9_]+$',
            default='PLUGIN_SECRET_SERVICE_TOKEN',
        ),
    },
    outputs={
        'value': Text('Value', sensitive=True),
    },
    permissions=['secrets.environment'],
)
async def execute(ctx):
    return {'value': ctx.environment_secret(ctx.config['variable'])}
