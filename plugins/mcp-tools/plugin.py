from contextlib import asynccontextmanager
from urllib.parse import urlsplit

from langchain_mcp_adapters.client import MultiServerMCPClient

from workflow_sdk import Resource, Text, resource


@resource(
    name='community.mcp-tools',
    version='1.0.0',
    title='MCP tools',
    description='Provide tools from an approved streamable HTTP MCP server.',
    category='AI resources',
    config={
        'url': Text('MCP server URL', description='Streamable HTTP endpoint for the MCP server.', default='https://mcp.internal/mcp'),
        'server_name': Text('Server name', description='Short label used to namespace tools from this server.', default='tools'),
    },
    inputs={
        'token': Text('Token', required=False, sensitive=True),
    },
    outputs={
        'tools': Resource('ai.tools/v1', 'Tools', sensitive=True),
    },
    permissions=['network.http'],
)
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
