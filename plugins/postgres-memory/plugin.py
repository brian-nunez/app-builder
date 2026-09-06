from contextlib import asynccontextmanager

from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver
from psycopg.conninfo import make_conninfo

from workflow_sdk import Boolean, Integer, Resource, Text, resource


@resource(
    name='community.postgres-memory',
    version='1.0.0',
    title='PostgreSQL memory',
    description='Provide an agent checkpointer using a separate, explicitly connected PostgreSQL database.',
    category='AI resources',
    config={
        'namespace': Text('Memory namespace', description='Logical namespace that separates checkpoint conversations.', default='default'),
        'initialize': Boolean('Initialize tables', description='Create checkpoint tables when the resource opens.', default=False),
        'sslmode': Text(
            'TLS mode',
            description='PostgreSQL sslmode used for the connection.',
            options=['verify-full', 'verify-ca', 'require', 'disable'],
            default='verify-full',
        ),
    },
    inputs={
        'host': Text('Host', sensitive=True),
        'port': Integer('Port', minimum=1, maximum=65535, required=False, sensitive=True),
        'database': Text('Database', sensitive=True),
        'username': Text('Username', sensitive=True),
        'password': Text('Password', sensitive=True),
    },
    outputs={
        'memory': Resource('memory.checkpointer/v1', 'Memory', sensitive=True),
    },
    permissions=['network.postgres'],
)
async def execute(ctx):
    ctx.check_host(ctx.inputs['host'])
    return {'memory': ctx.resource('memory.checkpointer/v1', {**ctx.inputs, **ctx.config})}


@asynccontextmanager
async def open_resource(ctx, value):
    ctx.check_host(value['host'])
    dsn = make_conninfo(host=value['host'], port=value.get('port', 5432), dbname=value['database'], user=value['username'], password=value['password'], sslmode=value['sslmode'], connect_timeout=10)
    async with AsyncPostgresSaver.from_conn_string(dsn) as saver:
        if value['initialize']:
            await saver.setup()
        yield saver
