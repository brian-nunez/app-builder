import asyncio
import importlib.util
import json
import os
import sys

import jsonschema
from opentelemetry import trace
from opentelemetry.propagate import extract
from opentelemetry.trace import Status, StatusCode

from .context import Context, PluginError, Telemetry
from .registry import PROTOCOL, catalog


async def invoke(request):
    manifest, directory = catalog(os.environ['WORKFLOW_PLUGIN_ROOT'])[(request['plugin'], request['version'])]
    if request['protocol'] != PROTOCOL:
        raise PluginError('artifact_mismatch')
    jsonschema.validate(request['config'], manifest['configSchema'])
    for port in manifest['inputs']:
        if port['name'] not in request['inputs']:
            if port.get('required'):
                raise PluginError('missing_input')
        else:
            value = request['inputs'][port['name']]
            if port.get('multiple'):
                if not isinstance(value, list):
                    raise PluginError('multiple_input_requires_array')
                for item in value:
                    jsonschema.validate(item, port['schema'])
            else:
                jsonschema.validate(value, port['schema'])
    spec = importlib.util.spec_from_file_location('installed_plugin', directory / 'plugin.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    telemetry = Telemetry()
    try:
        context = Context(request, manifest, telemetry.logger)
        with trace.get_tracer('workflow.runtime').start_as_current_span('plugin.handler', context=extract(request['context']), attributes=context.attributes) as span:
            try:
                context.log('execution.started')
                outputs = await module.execute(context)
                declared = {p['name'] for p in manifest['outputs']}
                if set(outputs) - declared:
                    raise PluginError('undeclared_output')
                for port in manifest['outputs']:
                    if port['name'] in outputs:
                        jsonschema.validate(outputs[port['name']], port['schema'])
                    elif port.get('required'):
                        raise PluginError('missing_output')
                context.log('execution.succeeded')
                return {'protocol': PROTOCOL, 'outputs': outputs}
            except Exception as error:
                code = error.code if isinstance(error, PluginError) else 'plugin_execution_failed'
                span.set_status(Status(StatusCode.ERROR, code))
                context.log('execution.failed', level='error')
                return {'protocol': PROTOCOL, 'outputs': {}, 'error': {'code': code, 'message': code, 'retryable': isinstance(error, PluginError) and error.retryable}}
    finally:
        telemetry.close()


def main():
    # Reserve stdout for the protocol; third-party prints are redirected to stderr.
    protocol_output = sys.stdout
    sys.stdout = sys.stderr
    try:
        result = asyncio.run(invoke(json.load(sys.stdin)))
    except Exception:
        result = {'protocol': PROTOCOL, 'outputs': {}, 'error': {'code': 'invalid_invocation', 'message': 'Invocation rejected', 'retryable': False}}
    protocol_output.write(json.dumps(result, allow_nan=False))


if __name__ == '__main__':
    main()
