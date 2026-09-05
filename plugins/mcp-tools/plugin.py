from contextlib import asynccontextmanager
from urllib.parse import urlsplit
from langchain_mcp_adapters.client import MultiServerMCPClient

async def execute(ctx):
    ctx.check_host(urlsplit(ctx.config['url']).hostname)
    return {'tools': ctx.resource('ai.tools/v1', {**ctx.config, **ctx.inputs})}

@asynccontextmanager
async def open_resource(ctx, value):
    ctx.check_host(urlsplit(value['url']).hostname)
    connection = {'transport': 'http', 'url': value['url']}
    if value.get('token'):
        connection['headers'] = {'Authorization': 'Bearer ' + value['token']}
    client = MultiServerMCPClient({value['server_name']: connection})
    yield await client.get_tools()
