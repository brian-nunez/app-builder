from urllib.parse import quote

async def execute(ctx):
    headers = {'X-Consul-Token': ctx.inputs['token']} if ctx.inputs.get('token') else {}
    response = await ctx.http('GET', ctx.config['url'].rstrip('/') + '/v1/kv/' + quote(ctx.config['key'], safe='/') + '?raw', headers=headers)
    return {'value': response.json() if ctx.config['parse_json'] else response.text}
