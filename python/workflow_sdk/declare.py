"""Declarative plugin definitions.

A plugin declares its identity, configuration, and ports once, next to the handler
that uses them. `workflow-plugin build` turns that declaration into the
`manifest.json` the platform reads. The manifest stays the wire format; it is no
longer the authoring surface.
"""

import re

PROTOCOL = 'workflow.plugin/v1'
KINDS = ('action', 'resource', 'trigger')
IDENTIFIER = re.compile(r'^[a-z][a-z0-9._-]{0,100}$')
VERSION = re.compile(r'^[0-9]+\.[0-9]+\.[0-9]+$')

_UNSET = object()


class DeclarationError(Exception):
    """A plugin declaration is malformed. Raised at import time, not run time."""


class Value:
    """A JSON Schema fragment plus the presentation metadata the editor needs.

    The same types describe configuration properties and ports. Where a flag only
    applies to one of the two, it is ignored for the other.
    """

    port_kind = 'data'
    resource_type = None

    def __init__(self, title=None, *, description=None, default=_UNSET, required=True,
                 sensitive=False, secret=False, multiple=False, side=None):
        self.title = title
        self.description = description
        self.default = default
        self.required = required
        self.sensitive = sensitive
        self.secret = secret
        self.multiple = multiple
        self.side = side

    def schema(self):
        return {}

    def _constraints(self, **pairs):
        return {key: value for key, value in pairs.items() if value is not None}


class Json(Value):
    """Any JSON value. Renders as a JSON editor."""


class Raw(Value):
    """A hand-written or generated JSON Schema fragment.

    The escape hatch for schemas the typed helpers cannot express — imported
    OpenAPI operations use it. Prefer a typed helper where one fits.
    """

    def __init__(self, json_schema, title=None, **kw):
        super().__init__(title, **kw)
        if not isinstance(json_schema, dict):
            raise DeclarationError('Raw schemas must be JSON objects')
        self._schema = json_schema

    def schema(self):
        return dict(self._schema)


class Text(Value):
    def __init__(self, title=None, *, pattern=None, min_length=None, max_length=None,
                 format=None, options=None, **kw):
        super().__init__(title, **kw)
        self.pattern = pattern
        self.min_length = min_length
        self.max_length = max_length
        self.format = format
        self.options = options

    def schema(self):
        return {'type': 'string', **self._constraints(
            enum=list(self.options) if self.options else None,
            pattern=self.pattern,
            minLength=self.min_length,
            maxLength=self.max_length,
            format=self.format,
        )}


class Integer(Value):
    def __init__(self, title=None, *, minimum=None, maximum=None, **kw):
        super().__init__(title, **kw)
        self.minimum = minimum
        self.maximum = maximum

    def schema(self):
        return {'type': 'integer', **self._constraints(minimum=self.minimum, maximum=self.maximum)}


class Number(Value):
    def __init__(self, title=None, *, minimum=None, maximum=None, **kw):
        super().__init__(title, **kw)
        self.minimum = minimum
        self.maximum = maximum

    def schema(self):
        return {'type': 'number', **self._constraints(minimum=self.minimum, maximum=self.maximum)}


class Boolean(Value):
    def schema(self):
        return {'type': 'boolean'}


class Object(Value):
    def __init__(self, title=None, *, properties=None, **kw):
        super().__init__(title, **kw)
        self.properties = properties

    def schema(self):
        if not self.properties:
            return {'type': 'object'}
        return {
            'type': 'object',
            'properties': {name: value.schema() for name, value in self.properties.items()},
        }


class List(Value):
    def __init__(self, title=None, *, items=None, min_items=None, unique=False, **kw):
        super().__init__(title, **kw)
        self.items = items
        self.min_items = min_items
        self.unique = unique

    def schema(self):
        return {'type': 'array', **self._constraints(
            items=self.items.schema() if self.items is not None else None,
            minItems=self.min_items,
            uniqueItems=True if self.unique else None,
        )}


class Resource(Value):
    """A port carrying a versioned resource descriptor rather than plain data."""

    port_kind = 'resource'

    def __init__(self, resource_type, title=None, **kw):
        super().__init__(title, **kw)
        self.resource_type = resource_type

    def schema(self):
        return {'type': 'object'}


class Metric:
    def __init__(self, name, kind, unit, description):
        self.name = name
        self.kind = kind
        self.unit = unit
        self.description = description

    def declaration(self):
        return {'name': self.name, 'kind': self.kind, 'unit': self.unit, 'description': self.description}


def Counter(name, unit, description):
    return Metric(name, 'counter', unit, description)


def Histogram(name, unit, description):
    return Metric(name, 'histogram', unit, description)


def _titled(name, value):
    if value.title:
        return value.title
    return name.replace('_', ' ').replace('-', ' ').capitalize()


def _config_schema(config):
    properties = {}
    required = []
    for name, value in config.items():
        if not isinstance(value, Value):
            raise DeclarationError(f'config property {name!r} must be a declared value type')
        if isinstance(value, Resource):
            raise DeclarationError(f'config property {name!r} cannot be a resource; use an input port')
        entry = dict(value.schema())
        entry['title'] = _titled(name, value)
        if value.description:
            entry['description'] = value.description
        if value.default is not _UNSET:
            entry['default'] = value.default
        if value.secret:
            entry['writeOnly'] = True
        properties[name] = entry
        if value.required:
            required.append(name)
    return {'type': 'object', 'properties': properties, 'required': required, 'additionalProperties': False}


def _ports(group, ports):
    result = []
    for name, value in ports.items():
        if not isinstance(value, Value):
            raise DeclarationError(f'{group} port {name!r} must be a declared value type')
        port = {
            'name': name,
            'title': _titled(name, value),
            'kind': value.port_kind,
            'schema': value.schema(),
            'required': bool(value.required),
            'sensitive': bool(value.sensitive),
        }
        if value.resource_type:
            port['resourceType'] = value.resource_type
        if value.multiple:
            port['multiple'] = True
        if value.side:
            port['side'] = value.side
        result.append(port)
    return result


def declaration(kind, *, name, version, title, description, category,
                config=None, inputs=None, outputs=None,
                permissions=(), metrics=(), log_fields=()):
    """Build the manifest body for one plugin. The digest is filled in by the build."""
    if kind not in KINDS:
        raise DeclarationError(f'plugin kind must be one of {", ".join(KINDS)}')
    if not IDENTIFIER.fullmatch(name or ''):
        raise DeclarationError(f'plugin name {name!r} must be a lowercase namespaced identifier')
    if not VERSION.fullmatch(version or ''):
        raise DeclarationError(f'plugin version {version!r} must be major.minor.patch')
    if not title:
        raise DeclarationError('plugin title is required')
    return {
        'protocol': PROTOCOL,
        'name': name,
        'version': version,
        'title': title,
        'description': description,
        'category': category,
        'kind': kind,
        'configSchema': _config_schema(config or {}),
        'inputs': _ports('input', inputs or {}),
        'outputs': _ports('output', outputs or {}),
        'permissions': list(permissions),
        'logFields': list(log_fields),
        'metrics': [metric.declaration() for metric in metrics],
        'digest': '',
    }


def _decorator(kind, options):
    def decorate(handler):
        handler.__workflow_plugin__ = declaration(kind, **options)
        return handler
    return decorate


def action(**options):
    """Declare an action plugin. Decorates the module's `execute` coroutine."""
    return _decorator('action', options)


def resource(**options):
    """Declare a resource plugin. The module also defines `open_resource`."""
    return _decorator('resource', options)


def trigger(**options):
    """Declare a trigger plugin. Its `event` input is supplied by the run."""
    return _decorator('trigger', options)


def declared_manifest(module):
    """Find the declaration a plugin module registered through its decorator."""
    for attribute in vars(module).values():
        manifest = getattr(attribute, '__workflow_plugin__', None)
        if manifest is not None:
            return manifest
    return None
