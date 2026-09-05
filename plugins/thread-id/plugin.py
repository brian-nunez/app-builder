import secrets
import time
from uuid import UUID

from workflow_sdk import PluginError


def uuid7():
    timestamp_ms = time.time_ns() // 1_000_000
    if not 0 <= timestamp_ms < 1 << 48:
        raise PluginError('uuid_timestamp_out_of_range')
    value = (timestamp_ms << 80) | (7 << 76) | (secrets.randbits(12) << 64)
    value |= (2 << 62) | secrets.randbits(62)
    return str(UUID(int=value))


async def execute(ctx):
    document = ctx.inputs['document']
    field = ctx.config['field']
    if field not in document:
        with ctx.span('thread_id.generate'):
            value = uuid7()
    else:
        value = document[field]
        if not isinstance(value, str) or not value.strip():
            raise PluginError('invalid_thread_id')
    return {'value': value}
