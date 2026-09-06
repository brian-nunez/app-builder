from workflow_sdk import Counter, Json, PluginError, Text, action


@action(
    name='community.json-field',
    version='1.0.0',
    title='JSON field',
    description='Extract a value from a document using a JSON Pointer. Sensitive data stays protected.',
    category='Data',
    config={
        'pointer': Text(
            'JSON Pointer',
            description='RFC 6901 path such as /customer/email. Leave blank for the full document.',
            default='',
            pattern='^($|/)',
        ),
    },
    inputs={
        'document': Json('Document', sensitive=True),
    },
    outputs={
        'value': Json('Value', sensitive=True),
    },
    log_fields=['value_type'],
    metrics=[Counter('json.fields.resolved', '{field}', 'Successfully resolved JSON fields')],
)
async def execute(ctx):
    value = ctx.inputs['document']
    pointer = ctx.config['pointer']
    with ctx.span('json.resolve'):
        if pointer:
            if not pointer.startswith('/'):
                raise PluginError('invalid_json_pointer')
            for segment in pointer[1:].split('/'):
                key = segment.replace('~1', '/').replace('~0', '~')
                try:
                    if isinstance(value, list):
                        if not key.isdigit() or (key.startswith('0') and key != '0'):
                            raise PluginError('invalid_array_index')
                        value = value[int(key)]
                    elif isinstance(value, dict):
                        value = value[key]
                    else:
                        raise PluginError('pointer_not_found')
                except (KeyError, IndexError):
                    raise PluginError('pointer_not_found') from None
    ctx.metric('json.fields.resolved', 1)
    ctx.log('json.field.resolved', value_type=type(value).__name__)
    return {'value': value}
