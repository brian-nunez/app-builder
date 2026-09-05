import argparse
import json
import re
from pathlib import Path

from .registry import PROTOCOL, catalog, digest


def main():
    parser = argparse.ArgumentParser(description='Create, seal, and validate workflow plugin packages.')
    sub = parser.add_subparsers(dest='command', required=True)
    create = sub.add_parser('new')
    create.add_argument('name')
    create.add_argument('--root', default='plugins')
    for command in ('seal', 'validate'):
        item = sub.add_parser(command)
        item.add_argument('--root', default='plugins')
    imported = sub.add_parser('import-openapi')
    imported.add_argument('document')
    imported.add_argument('--operation', required=True)
    imported.add_argument('--name', required=True)
    imported.add_argument('--base-url', required=True)
    imported.add_argument('--root', default='plugins')
    args = parser.parse_args()
    if args.command == 'import-openapi':
        from .openapi import import_operation
        print(import_operation(args.document, args.operation, args.name, args.base_url, args.root))
        return
    if args.command == 'new':
        if not re.fullmatch(r'[a-z][a-z0-9.-]{1,100}', args.name):
            parser.error('Name must be a lowercase namespaced identifier')
        directory = Path(args.root) / args.name
        directory.mkdir(parents=True, exist_ok=False)
        manifest = {'protocol': PROTOCOL, 'name': args.name, 'version': '1.0.0', 'title': args.name, 'description': 'Transforms a value without changing its representation.', 'category': 'Custom', 'kind': 'action', 'configSchema': {'type': 'object', 'properties': {}, 'additionalProperties': False}, 'inputs': [{'name': 'value', 'title': 'Value', 'kind': 'data', 'required': True, 'schema': {}}], 'outputs': [{'name': 'value', 'title': 'Value', 'kind': 'data', 'required': True, 'schema': {}}], 'permissions': [], 'metrics': [], 'logFields': [], 'digest': ''}
        (directory / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        (directory / 'plugin.py').write_text("async def execute(ctx):\n    return {'value': ctx.inputs['value']}\n")
        manifest['digest'] = digest(directory)
        (directory / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        print(directory)
    elif args.command == 'seal':
        for path in Path(args.root).glob('*/manifest.json'):
            manifest = json.loads(path.read_text())
            manifest['digest'] = digest(path.parent)
            path.write_text(json.dumps(manifest, indent=2) + '\n')
    else:
        print(json.dumps([m for m, _ in catalog(args.root).values()], indent=2))


if __name__ == '__main__':
    main()
