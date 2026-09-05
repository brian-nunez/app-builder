import contextlib
import json
import logging
import os
import re
from urllib.parse import urlsplit

import httpx
from opentelemetry import metrics, trace
from opentelemetry.propagate import inject
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


class PluginError(Exception):
    def __init__(self, code, message='Plugin execution failed', retryable=False):
        super().__init__(message)
        self.code = code if re.fullmatch(r'[a-z][a-z0-9_]{0,79}', code) else 'plugin_execution_failed'
        self.retryable = retryable


class Telemetry:
    def __init__(self):
        resource = Resource.create({'service.name': 'workflow-python-worker'})
        self.traces = TracerProvider(resource=resource)
        self.traces.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
        trace.set_tracer_provider(self.traces)
        self.metrics = MeterProvider(resource=resource, metric_readers=[
            PeriodicExportingMetricReader(OTLPMetricExporter())])
        metrics.set_meter_provider(self.metrics)
        self.logs = LoggerProvider(resource=resource)
        self.logs.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter()))
        set_logger_provider(self.logs)
        self.logger = logging.getLogger('workflow.plugin')
        self.logger.setLevel(logging.INFO)
        self.logger.addHandler(LoggingHandler(logger_provider=self.logs))

    def close(self):
        self.logs.shutdown()
        self.metrics.shutdown()
        self.traces.shutdown()


class Context:
    def __init__(self, request, manifest, logger):
        self.config = request['config']
        self.inputs = request['inputs']
        self.execution = request['context']
        self.idempotency_key = self.execution['idempotencyKey']
        self.manifest = manifest
        self._logger = logger
        self._log_fields = set(request.get('logFields') or []) & set(manifest.get('logFields') or [])
        self.attributes = {
            'workflow.id': self.execution['workflowId'],
            'workflow.name': self.execution['workflowName'],
            'workflow.revision': self.execution['revision'],
            'workflow.run.id': self.execution['runId'],
            'workflow.node.id': self.execution['nodeId'],
            'workflow.node.name': self.execution['nodeName'],
            'workflow.attempt': self.execution['attempt'],
            'plugin.name': manifest['name'],
            'plugin.version': manifest['version'],
        }
        self.metric_attributes = {k: self.attributes[k] for k in ('workflow.name', 'plugin.name', 'plugin.version')}
        self._instruments = {}
        meter = metrics.get_meter(manifest['name'], manifest['version'])
        for spec in manifest.get('metrics', []):
            factory = meter.create_counter if spec['kind'] == 'counter' else meter.create_histogram
            self._instruments[spec['name']] = (spec['kind'], factory(spec['name'], unit=spec['unit'], description=spec['description']))
        self._secrets = []
        for name, schema in manifest['configSchema'].get('properties', {}).items():
            if schema.get('writeOnly') and name in self.config:
                self._collect_secrets(self.config[name])
        for port in manifest['inputs']:
            if port.get('sensitive') and port['name'] in self.inputs:
                self._collect_secrets(self.inputs[port['name']])

    def _collect_secrets(self, value):
        if isinstance(value, str) and value:
            self._secrets.append(value)
        elif isinstance(value, dict):
            for item in value.values():
                self._collect_secrets(item)
        elif isinstance(value, list):
            for item in value:
                self._collect_secrets(item)

    def _redact(self, value):
        text = json.dumps(value, default=str)
        for secret in sorted(self._secrets, key=len, reverse=True):
            text = text.replace(json.dumps(secret)[1:-1], '[REDACTED]').replace(secret, '[REDACTED]')
        return text[:4096]

    def log(self, event, *, level='info', **fields):
        # Event names are developer constants; payload fields require workflow opt-in.
        attrs = dict(self.attributes)
        for key, value in fields.items():
            if key in self._log_fields:
                attrs['plugin.data.' + key] = self._redact(value)
        self._logger.log({'debug': logging.DEBUG, 'info': logging.INFO, 'warning': logging.WARNING, 'error': logging.ERROR}.get(level, logging.INFO), self._redact(event), extra=attrs)

    @contextlib.contextmanager
    def span(self, name):
        with trace.get_tracer(self.manifest['name'], self.manifest['version']).start_as_current_span(name, attributes=self.attributes) as span:
            yield span

    def metric(self, name, value):
        if name not in self._instruments:
            raise PluginError('undeclared_metric')
        kind, instrument = self._instruments[name]
        (instrument.add if kind == 'counter' else instrument.record)(value, self.metric_attributes)

    def check_host(self, host):
        allowed = {h.strip().lower() for h in os.environ.get('WORKFLOW_ALLOWED_HOSTS', '').split(',') if h.strip()}
        if '*' not in allowed and host.lower() not in allowed:
            raise PluginError('host_not_allowed')

    async def http(self, method, url, *, headers=None, json_body=None, content=None):
        if 'network.http' not in self.manifest['permissions']:
            raise PluginError('permission_denied')
        parsed = urlsplit(url)
        if parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username or parsed.password:
            raise PluginError('invalid_url')
        self.check_host(parsed.hostname)
        with self.span('http.request'):
            headers = dict(headers or {})
            inject(headers)
            async with httpx.AsyncClient(timeout=45, follow_redirects=False, trust_env=False) as client:
                async with client.stream(method, url, headers=headers, json=json_body, content=content) as response:
                    body = bytearray()
                    async for chunk in response.aiter_bytes():
                        body.extend(chunk)
                        if len(body) > 4 * 1024 * 1024:
                            raise PluginError('response_too_large')
                    if response.status_code >= 400:
                        raise PluginError('http_' + str(response.status_code), retryable=response.status_code == 429 or response.status_code >= 500)
                    # aiter_bytes already decoded the transport's compressed body.
                    decoded_headers = response.headers.copy()
                    for header in ('content-encoding', 'content-length', 'transfer-encoding'):
                        decoded_headers.pop(header, None)
                    return httpx.Response(response.status_code, headers=decoded_headers, content=bytes(body))

    def environment_secret(self, name):
        if 'secrets.environment' not in self.manifest['permissions']:
            raise PluginError('permission_denied')
        if not name.startswith('PLUGIN_SECRET_'):
            raise PluginError('invalid_secret_reference')
        value = os.environ.get(name)
        if value is None:
            raise PluginError('secret_unavailable')
        self._collect_secrets(value)
        return value

    def resource(self, resource_type, value):
        return {'protocol': 'workflow.resource/v1', 'type': resource_type, 'plugin': self.manifest['name'], 'version': self.manifest['version'], 'digest': self.manifest['digest'], 'value': value}

    @contextlib.asynccontextmanager
    async def resolve(self, descriptor):
        import importlib.util
        from .registry import catalog
        if descriptor.get('protocol') != 'workflow.resource/v1':
            raise PluginError('invalid_resource_protocol')
        manifest, directory = catalog(os.environ['WORKFLOW_PLUGIN_ROOT'])[(descriptor['plugin'], descriptor['version'])]
        if descriptor['digest'] != manifest['digest']:
            raise PluginError('resource_artifact_mismatch')
        if not any(p.get('resourceType') == descriptor['type'] for p in manifest['outputs']):
            raise PluginError('undeclared_resource_type')
        spec = importlib.util.spec_from_file_location('resource_plugin', directory / 'plugin.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        request = {'config': {}, 'inputs': {}, 'context': self.execution, 'logFields': []}
        provider_context = Context(request, manifest, self._logger)
        provider_context._collect_secrets(descriptor['value'])
        async with module.open_resource(provider_context, descriptor['value']) as resource:
            yield resource
