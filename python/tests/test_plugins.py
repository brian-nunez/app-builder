import asyncio
import importlib.util
import json
import sys
import unittest
from contextlib import asynccontextmanager, contextmanager
from pathlib import Path

import jsonschema


ROOT = Path(__file__).resolve().parents[2]
PLUGINS = ROOT / 'plugins'


class Response:
    status_code = 200
    content = b'{"ok":true}'
    text = '{"ok":true}'
    headers = {'content-type': 'application/json'}

    def __init__(self, payload=None):
        self.payload = payload or {'ok': True}

    def json(self):
        return self.payload


class Context:
    def __init__(self, manifest, config, inputs):
        self.manifest = manifest
        self.config = config
        self.inputs = inputs
        self.execution = {'runId': 'run-1', 'workflowId': 'workflow-1'}
        self.idempotency_key = 'run-1:node-1'
        self.requests = []

    async def http(self, method, url, **kwargs):
        self.requests.append((method, url, kwargs))
        if self.manifest['name'] == 'community.vault-kv':
            return Response({'data': {'data': {self.config['field']: 'secret'}}})
        return Response()

    def check_host(self, host):
        if not host:
            raise AssertionError('plugin did not resolve a hostname')

    def resource(self, resource_type, value):
        return {'type': resource_type, 'value': value}

    def environment_secret(self, name):
        return f'secret:{name}'

    def metric(self, *args, **kwargs):
        return None

    def log(self, *args, **kwargs):
        return None

    @contextmanager
    def span(self, _name):
        yield

    @asynccontextmanager
    async def resolve(self, _descriptor):
        yield object()


class AgentMessage:
    text = 'done'
    tool_calls = []


class Agent:
    async def ainvoke(self, *_args, **_kwargs):
        return {'messages': [AgentMessage()]}


def sample(schema):
    if 'default' in schema:
        return schema['default']
    if schema.get('enum'):
        return schema['enum'][0]
    return {
        'string': 'value',
        'integer': 1,
        'number': 1,
        'boolean': False,
        'object': {},
        'array': [],
    }.get(schema.get('type'), {})


def load_plugin(directory):
    name = 'tested_plugin_' + directory.name.replace('-', '_').replace('.', '_')
    spec = importlib.util.spec_from_file_location(name, directory / 'plugin.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class PluginTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.packages = []
        for path in sorted(PLUGINS.glob('*/manifest.json')):
            manifest = json.loads(path.read_text())
            cls.packages.append((path.parent, manifest, load_plugin(path.parent)))

    def test_every_plugin_has_clear_valid_configuration(self):
        self.assertEqual(16, len(self.packages))
        identities = set()
        for directory, manifest, _module in self.packages:
            identity = (manifest['name'], manifest['version'])
            self.assertNotIn(identity, identities)
            identities.add(identity)
            self.assertTrue(manifest['title'], directory)
            self.assertTrue(manifest['description'], directory)
            config = {name: sample(schema) for name, schema in manifest['configSchema'].get('properties', {}).items()}
            jsonschema.validate(config, manifest['configSchema'])
            for name, schema in manifest['configSchema'].get('properties', {}).items():
                self.assertTrue(schema.get('title'), f'{directory.name}.{name} needs a title')
                self.assertTrue(schema.get('description'), f'{directory.name}.{name} needs guidance')
            for port in manifest['inputs'] + manifest['outputs']:
                self.assertTrue(port['title'], f'{directory.name}.{port["name"]}')

    def test_every_plugin_imports_and_executes_a_smoke_case(self):
        async def run():
            for directory, manifest, module in self.packages:
                config = {name: sample(schema) for name, schema in manifest['configSchema'].get('properties', {}).items()}
                inputs = {port['name']: sample(port['schema']) for port in manifest['inputs'] if port.get('required')}
                name = manifest['name']
                if name == 'community.json-field':
                    inputs['document'] = {'value': 'ok'}
                elif name == 'community.thread-id':
                    inputs['document'] = {}
                elif name == 'community.openapi-operation':
                    config['operation_id'] = 'health'
                    config['document'] = {'paths': {'/health': {'get': {'operationId': 'health'}}}}
                elif name == 'community.langchain-agent':
                    module.create_agent = lambda **_kwargs: Agent()
                    inputs['prompt'] = 'hello'
                    inputs['model'] = {'type': 'ai.chat-model/v1'}
                context = Context(manifest, config, inputs)
                result = await module.execute(context)
                self.assertIsInstance(result, dict, directory.name)
                self.assertEqual({port['name'] for port in manifest['outputs']}, set(result), directory.name)

        asyncio.run(run())


if __name__ == '__main__':
    unittest.main()
