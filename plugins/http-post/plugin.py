from workflow_sdk import Integer, Json, Text, action


@action(
    name='community.http-post',
    version='1.0.0',
    title='HTTP POST',
    description='Send a JSON payload to an explicitly configured endpoint.',
    category='HTTP',
    config={
        'url': Text(
            'Destination URL',
            description='HTTPS endpoint that receives the JSON body.',
            default='https://service.internal/events',
        ),
    },
    inputs={
        'body': Json('Body', sensitive=True),
        'token': Text('Token', required=False, sensitive=True),
    },
    outputs={
        'response': Json('Response', sensitive=True),
        'status': Integer('Status'),
    },
    permissions=['network.http'],
)
async def execute(ctx):
    headers = {'Idempotency-Key': ctx.idempotency_key}
    if ctx.inputs.get('token'):
        headers['Authorization'] = 'Bearer ' + ctx.inputs['token']
    response = await ctx.http('POST', ctx.config['url'], headers=headers, json_body=ctx.inputs['body'])
    return {'response': response.json() if response.content else None, 'status': response.status_code}
