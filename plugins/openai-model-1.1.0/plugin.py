from contextlib import asynccontextmanager
from urllib.parse import urlsplit

from langchain_openai import ChatOpenAI

from workflow_sdk import Number, Resource, Text, resource


@resource(
    name='community.openai-model',
    version='1.1.0',
    title='OpenAI-compatible model',
    description='Provide a chat model through the standard model resource contract.',
    category='AI resources',
    config={
        'model': Text('Default model', description='Model ID used when the model input is not connected.', default='gpt-4.1-mini'),
        'base_url': Text('API base URL', description='OpenAI-compatible API root, including /v1 when required.', default='https://api.openai.com/v1'),
        'temperature': Number('Temperature', description='Response randomness from 0 (focused) to 2 (varied).', minimum=0, maximum=2, default=0.2),
    },
    inputs={
        'api_key': Text('Api Key', sensitive=True),
        'model': Text('Model ID', min_length=1, required=False, sensitive=True),
    },
    outputs={
        'model': Resource('ai.chat-model/v1', 'Model', sensitive=True),
    },
    permissions=['network.http'],
)
async def execute(ctx):
    ctx.check_host(urlsplit(ctx.config['base_url']).hostname)
    return {'model': ctx.resource('ai.chat-model/v1', {**ctx.config, **ctx.inputs})}


@asynccontextmanager
async def open_resource(ctx, value):
    ctx.check_host(urlsplit(value['base_url']).hostname)
    yield ChatOpenAI(model=value['model'], base_url=value['base_url'], api_key=value['api_key'], temperature=value['temperature'], timeout=60, max_retries=1)
