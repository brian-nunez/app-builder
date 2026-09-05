async def execute(ctx):
    headers = {'Idempotency-Key': ctx.idempotency_key}
    if ctx.inputs.get('token'):
        headers['Authorization'] = 'Bearer ' + ctx.inputs['token']
    response = await ctx.http('POST', ctx.config['url'], headers=headers, json_body=ctx.inputs['body'])
    return {'response': response.json() if response.content else None, 'status': response.status_code}
