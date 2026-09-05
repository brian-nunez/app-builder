from contextlib import asynccontextmanager
from urllib.parse import urlsplit
from langchain_openai import ChatOpenAI

async def execute(ctx):
    ctx.check_host(urlsplit(ctx.config['base_url']).hostname)
    return {'model': ctx.resource('ai.chat-model/v1', {**ctx.config, **ctx.inputs})}

@asynccontextmanager
async def open_resource(ctx, value):
    ctx.check_host(urlsplit(value['base_url']).hostname)
    yield ChatOpenAI(model=value['model'], base_url=value['base_url'], api_key=value['api_key'], temperature=value['temperature'], timeout=60, max_retries=1)
