from contextlib import asynccontextmanager
from langgraph.checkpoint.memory import InMemorySaver

async def execute(ctx):
    return {'memory': ctx.resource('memory.checkpointer/v1', {})}

@asynccontextmanager
async def open_resource(ctx, value):
    with InMemorySaver() as saver:
        yield saver
