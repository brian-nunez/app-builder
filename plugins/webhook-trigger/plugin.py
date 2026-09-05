async def execute(ctx):
    return {'body': ctx.inputs.get('event', {})}
