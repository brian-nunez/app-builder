from contextlib import AsyncExitStack

from langchain.agents import create_agent

from workflow_sdk import Counter, Integer, Resource, Text, action


@action(
    name='community.langchain-agent',
    version='1.2.0',
    title='LangChain agent',
    description='Compose a model, optional memory, and tools into a LangChain create_agent invocation.',
    category='AI',
    config={
        'system_prompt': Text('System prompt', description='Base instructions used when the system prompt input is not connected.', default='You are a helpful assistant.'),
        'thread_id': Text('Default thread ID', description='Stable conversation ID. Leave blank to use the run ID.', default=''),
        'recursion_limit': Integer('Recursion limit', description='Maximum agent graph steps allowed for one invocation.', minimum=1, maximum=100, default=25),
    },
    inputs={
        'prompt': Text('Prompt', sensitive=True),
        'model': Resource('ai.chat-model/v1', 'Model', sensitive=True),
        'memory': Resource('memory.checkpointer/v1', 'Memory', required=False, sensitive=True),
        'tools': Resource('ai.tools/v1', 'Tools', required=False, sensitive=True, multiple=True),
        'system_prompt': Text('System prompt', min_length=1, required=False, sensitive=True),
        'thread_id': Text('Thread ID', min_length=1, required=False, sensitive=True),
    },
    outputs={
        'response': Text('Response', sensitive=True),
        'tool_calls': Integer('Tool calls'),
    },
    log_fields=['message_count'],
    metrics=[Counter('agent.messages', '{message}', 'Messages produced or consumed by an agent invocation')],
)
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
