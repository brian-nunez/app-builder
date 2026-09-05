from contextlib import AsyncExitStack
from langchain.agents import create_agent

async def execute(ctx):
    async with AsyncExitStack() as stack:
        model = await stack.enter_async_context(ctx.resolve(ctx.inputs['model']))
        memory = await stack.enter_async_context(ctx.resolve(ctx.inputs['memory'])) if ctx.inputs.get('memory') else None
        tools = []
        for descriptor in ctx.inputs.get('tools', []):
            tools.extend(await stack.enter_async_context(ctx.resolve(descriptor)))
        agent = create_agent(model=model, tools=tools, checkpointer=memory, system_prompt=ctx.inputs.get('system_prompt', ctx.config['system_prompt']))
        namespace = ctx.inputs.get('memory', {}).get('value', {}).get('namespace', 'default')
        thread_id = ctx.inputs.get('thread_id') or ctx.config['thread_id'] or ctx.execution['runId']
        with ctx.span('agent.invoke'):
            result = await agent.ainvoke({'messages': [{'role': 'user', 'content': ctx.inputs['prompt']}]}, config={'configurable': {'thread_id': ctx.execution['workflowId'] + ':' + namespace + ':' + thread_id}, 'recursion_limit': ctx.config['recursion_limit']})
        ctx.metric('agent.messages', len(result['messages']))
        return {'response': result['messages'][-1].text, 'tool_calls': sum(len(getattr(message, 'tool_calls', [])) for message in result['messages'])}
