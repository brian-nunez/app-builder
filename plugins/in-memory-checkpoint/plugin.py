from contextlib import asynccontextmanager

from langgraph.checkpoint.memory import InMemorySaver

from workflow_sdk import Resource, resource


@resource(
    name='community.in-memory-checkpoint',
    version='1.0.0',
    title='In-memory checkpoint',
    description='Run-scoped LangGraph checkpoint; discarded after each agent invocation.',
    category='AI resources',
    outputs={
        'memory': Resource('memory.checkpointer/v1', 'Memory', sensitive=True),
    },
)
async def execute(ctx):
    return {'memory': ctx.resource('memory.checkpointer/v1', {})}


@asynccontextmanager
async def open_resource(ctx, value):
    with InMemorySaver() as saver:
        yield saver
