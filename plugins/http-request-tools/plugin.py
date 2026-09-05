from contextlib import asynccontextmanager
from urllib.parse import urlsplit
from langchain_core.tools import StructuredTool
from workflow_sdk import PluginError

async def execute(ctx):
    parsed = urlsplit(ctx.config['base_url'])
    if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise PluginError('invalid_tool_base_url')
    ctx.check_host(parsed.hostname)
    return {'tools': ctx.resource('ai.tools/v1', ctx.config)}

@asynccontextmanager
async def open_resource(ctx, value):
    async def http_request(path: str, method: str = 'GET', body: dict | None = None) -> dict:
        """Make an HTTP request to the configured service. Use a relative path beginning with /; JSON body is optional."""
        parsed = urlsplit(path)
        if not path.startswith('/') or path.startswith('//') or parsed.scheme or parsed.netloc or parsed.fragment or '\\' in path:
            raise PluginError('invalid_tool_path')
        method = method.upper()
        if method not in value['methods']:
            raise PluginError('tool_method_not_allowed')
        with ctx.span('tool.http_request'):
            response = await ctx.http(method, value['base_url'].rstrip('/') + path, json_body=body)
            try:
                payload = response.json()
            except ValueError:
                payload = response.text
            return {'status': response.status_code, 'body': payload}
    yield [StructuredTool.from_function(coroutine=http_request)]
