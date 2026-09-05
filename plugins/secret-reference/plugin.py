async def execute(ctx):
    return {'value': ctx.environment_secret(ctx.config['variable'])}
