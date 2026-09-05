from urllib.parse import quote

async def execute(ctx):
    c = ctx.config
    url = c['url'].rstrip('/') + '/v1/' + quote(c['mount'], safe='') + '/data/' + quote(c['path'], safe='/')
    response = await ctx.http('GET', url, headers={'X-Vault-Token': ctx.inputs['token']})
    return {'value': response.json()['data']['data'][c['field']]}
