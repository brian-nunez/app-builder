"""A test harness for plugin packages.

Plugins are exercised through the same contract the worker enforces: the manifest
generated from the declaration validates the configuration and inputs going in and
the outputs coming back. A plugin that passes here fails for real reasons, not
because a test stubbed past its interface.

    from workflow_sdk.testing import harness

    async def test_http_post():
        plugin = harness('plugins/http-post', responses={'POST': {'ok': True}})
        result = await plugin.execute(config={'url': 'https://service.internal/x'},
                                      inputs={'body': {'id': 1}})
        assert result['status'] == 200
"""

import contextlib
import json
from pathlib import Path

import jsonschema

from .context import PluginError
from .registry import declared, load_module, read


class Response:
    """The subset of httpx.Response the SDK's ctx.http contract exposes."""

    def __init__(self, payload=None, *, status_code=200, headers=None, text=None):
        self._payload = payload
        self.status_code = status_code
        self.headers = headers or {'content-type': 'application/json'}
        if text is not None:
            self.text = text
            self.content = text.encode()
        else:
            self.text = json.dumps(payload) if payload is not None else ''
            self.content = self.text.encode()
        self.encoding = 'utf-8'

    def json(self):
        if self._payload is None:
            raise ValueError('response body is not JSON')
        return self._payload


class StubContext:
    """A Context that records instead of reaching the network or a collector."""

    def __init__(self, manifest, config, inputs, *, responses=None, secrets=None, resources=None):
        self.manifest = manifest
        self.config = config
        self.inputs = inputs
        self.execution = {
            'workflowId': 'workflow-1',
            'workflowName': 'Test workflow',
            'revision': 1,
            'runId': 'run-1',
            'nodeId': 'node-1',
            'nodeName': 'Test node',
            'attempt': 1,
            'idempotencyKey': 'run-1:node-1',
            'traceparent': '',
        }
        self.idempotency_key = self.execution['idempotencyKey']
        self.requests = []
        self.logs = []
        self.metrics = []
        self.hosts = []
        self._responses = responses or {}
        self._secrets = secrets or {}
        self._resources = resources or {}

    async def http(self, method, url, *, headers=None, json_body=None, content=None):
        if 'network.http' not in self.manifest['permissions']:
            raise PluginError('permission_denied')
        self.requests.append({'method': method, 'url': url, 'headers': headers or {}, 'json': json_body, 'content': content})
        reply = self._responses.get(method, self._responses.get('*', {'ok': True}))
        if callable(reply):
            reply = reply(method, url, headers or {}, json_body)
        return reply if isinstance(reply, Response) else Response(reply)

    def check_host(self, host):
        if not host:
            raise AssertionError('plugin did not resolve a hostname before calling out')
        self.hosts.append(host)

    def environment_secret(self, name):
        if 'secrets.environment' not in self.manifest['permissions']:
            raise PluginError('permission_denied')
        return self._secrets.get(name, f'secret:{name}')

    def resource(self, resource_type, value):
        return {
            'protocol': 'workflow.resource/v1',
            'type': resource_type,
            'plugin': self.manifest['name'],
            'version': self.manifest['version'],
            'digest': self.manifest['digest'],
            'value': value,
        }

    def metric(self, name, value):
        declared_names = {item['name'] for item in self.manifest.get('metrics', [])}
        if name not in declared_names:
            raise PluginError('undeclared_metric')
        self.metrics.append((name, value))

    def log(self, event, *, level='info', **fields):
        allowed = set(self.manifest.get('logFields') or [])
        undeclared = set(fields) - allowed
        if undeclared:
            raise AssertionError(f'{event}: undeclared log field(s) {sorted(undeclared)}')
        self.logs.append({'event': event, 'level': level, 'fields': fields})

    @contextlib.contextmanager
    def span(self, _name):
        yield None

    @contextlib.asynccontextmanager
    async def resolve(self, descriptor):
        key = descriptor.get('type') if isinstance(descriptor, dict) else None
        yield self._resources.get(key, object())


def sample(schema):
    """An example value satisfying a port or configuration schema."""
    if 'default' in schema:
        return schema['default']
    if schema.get('enum'):
        return schema['enum'][0]
    if schema.get('type') == 'array' and schema.get('items'):
        return [sample(schema['items'])] if schema.get('minItems') else []
    return {
        'string': 'value',
        'integer': 1,
        'number': 1,
        'boolean': False,
        'object': {},
        'array': [],
    }.get(schema.get('type'), {})


class Harness:
    """One installed package, loaded and callable through its declared contract."""

    def __init__(self, directory, **stubs):
        self.directory = Path(directory)
        self.module = load_module(self.directory, 'harness_' + self.directory.name.replace('-', '_').replace('.', '_'))
        try:
            self.manifest = read(self.directory)
        except Exception:
            # An unbuilt package is still testable; the declaration is the contract.
            self.manifest = {**declared(self.directory), 'digest': '0' * 64}
        self.stubs = stubs
        self.context = None

    def defaults(self):
        """Configuration and inputs filled from the declared schemas."""
        config = {name: sample(schema) for name, schema in self.manifest['configSchema'].get('properties', {}).items()}
        inputs = {port['name']: sample(port['schema']) for port in self.manifest['inputs'] if port.get('required')}
        return config, inputs

    async def execute(self, *, config=None, inputs=None, **stubs):
        base_config, base_inputs = self.defaults()
        config = {**base_config, **(config or {})}
        inputs = {**base_inputs, **(inputs or {})}
        jsonschema.validate(config, self.manifest['configSchema'])
        for port in self.manifest['inputs']:
            if port['name'] not in inputs:
                if port.get('required'):
                    raise AssertionError(f'missing required input {port["name"]!r}')
                continue
            value = inputs[port['name']]
            if port.get('multiple'):
                assert isinstance(value, list), f'{port["name"]} declares multiple: pass a list'
                for item in value:
                    jsonschema.validate(item, port['schema'])
            else:
                jsonschema.validate(value, port['schema'])
        self.context = StubContext(self.manifest, config, inputs, **{**self.stubs, **stubs})
        outputs = await self.module.execute(self.context)
        declared_ports = {port['name'] for port in self.manifest['outputs']}
        undeclared = set(outputs) - declared_ports
        if undeclared:
            raise AssertionError(f'undeclared output(s) {sorted(undeclared)}')
        for port in self.manifest['outputs']:
            if port['name'] in outputs:
                jsonschema.validate(outputs[port['name']], port['schema'])
            elif port.get('required'):
                raise AssertionError(f'missing required output {port["name"]!r}')
        return outputs

    @contextlib.asynccontextmanager
    async def open_resource(self, value):
        """Open the package's resource hook, as a consuming plugin would."""
        context = StubContext(self.manifest, {}, {}, **self.stubs)
        async with self.module.open_resource(context, value) as opened:
            yield opened


def harness(directory, **stubs):
    return Harness(directory, **stubs)
