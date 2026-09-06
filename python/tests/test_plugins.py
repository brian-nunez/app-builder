import asyncio
import unittest
from pathlib import Path

from workflow_sdk.registry import catalog, declared, digest, read
from workflow_sdk.testing import Response, harness

ROOT = Path(__file__).resolve().parents[2]
PLUGINS = ROOT / 'plugins'


class AgentMessage:
    text = 'done'
    tool_calls = []


class Agent:
    async def ainvoke(self, *_args, **_kwargs):
        return {'messages': [AgentMessage()]}


# Inputs a schema cannot infer, keyed by plugin name.
SMOKE = {
    'community.json-field': {'inputs': {'document': {'value': 'ok'}}},
    'community.thread-id': {'inputs': {'document': {}}},
    'community.openapi-operation': {
        'config': {'operation_id': 'health', 'document': {'paths': {'/health': {'get': {'operationId': 'health'}}}}},
    },
    'community.langchain-agent': {
        'inputs': {'prompt': 'hello', 'model': {'type': 'ai.chat-model/v1'}},
    },
    'community.vault-kv': {
        'responses': {'GET': Response({'data': {'data': {'password': 'secret'}}})},
    },
}


def packages():
    return sorted(path.parent for path in PLUGINS.glob('*/manifest.json'))


class ManifestTests(unittest.TestCase):
    def test_every_manifest_is_generated_from_its_declaration(self):
        for directory in packages():
            with self.subTest(directory.name):
                expected = {**declared(directory), 'digest': digest(directory)}
                self.assertEqual(
                    expected, read(directory),
                    f'{directory.name}: manifest.json is stale; run "make plugins-build"',
                )

    def test_the_catalog_verifies_every_sealed_digest(self):
        installed = catalog(PLUGINS, verify_digests=True)
        self.assertTrue(installed)
        self.assertEqual(len(installed), len(packages()), 'duplicate plugin identities')

    def test_every_plugin_explains_itself_in_the_editor(self):
        for directory in packages():
            manifest = read(directory)
            with self.subTest(manifest['name']):
                self.assertTrue(manifest['title'])
                self.assertTrue(manifest['description'])
                for name, schema in manifest['configSchema'].get('properties', {}).items():
                    self.assertTrue(schema.get('title'), f'{name} needs a title')
                    self.assertTrue(schema.get('description'), f'{name} needs guidance')
                for port in manifest['inputs'] + manifest['outputs']:
                    self.assertTrue(port['title'], port['name'])


class ExecutionTests(unittest.TestCase):
    def test_every_plugin_runs_a_smoke_case_through_its_contract(self):
        async def run():
            for directory in packages():
                plugin = harness(directory)
                name = plugin.manifest['name']
                overrides = dict(SMOKE.get(name, {}))
                if name == 'community.langchain-agent':
                    plugin.module.create_agent = lambda **_kwargs: Agent()
                stubs = {'responses': overrides.pop('responses')} if 'responses' in overrides else {}
                with self.subTest(name):
                    outputs = await plugin.execute(**overrides, **stubs)
                    self.assertEqual(
                        {port['name'] for port in plugin.manifest['outputs']},
                        set(outputs),
                        directory.name,
                    )

        asyncio.run(run())

    def test_a_plugin_cannot_return_an_undeclared_output(self):
        async def run():
            plugin = harness(PLUGINS / 'static-config')
            original = plugin.module.execute

            async def surprising(ctx):
                return {**await original(ctx), 'extra': 1}

            plugin.module.execute = surprising
            with self.assertRaises(AssertionError):
                await plugin.execute()

        asyncio.run(run())

    def test_http_permission_is_enforced_by_the_manifest(self):
        async def run():
            plugin = harness(PLUGINS / 'static-config')
            await plugin.execute()
            from workflow_sdk import PluginError
            with self.assertRaises(PluginError):
                await plugin.context.http('GET', 'https://example.internal')

        asyncio.run(run())


if __name__ == '__main__':
    unittest.main()
