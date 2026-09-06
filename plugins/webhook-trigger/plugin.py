from workflow_sdk import Json, trigger


@trigger(
    name='community.webhook-trigger',
    version='1.0.0',
    title='Inbound webhook',
    description='Accept a JSON POST through a published workflow trigger binding.',
    category='HTTP',
    inputs={
        'event': Json('Event', required=False, sensitive=True),
    },
    outputs={
        'body': Json('Body', sensitive=True),
    },
)
async def execute(ctx):
    return {'body': ctx.inputs.get('event', {})}
