from contextlib import asynccontextmanager
from psycopg.conninfo import make_conninfo
from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver

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
