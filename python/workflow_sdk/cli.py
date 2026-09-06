import argparse
import json
import sys
from pathlib import Path

from .declare import IDENTIFIER
from .registry import ArtifactError, build, catalog, declared, digest, drift, read, verify

SCAFFOLD = '''from workflow_sdk import Text, action


@action(
    name={name!r},
    version='1.0.0',
    title={title!r},
    description='Transforms a value without changing its representation.',
    category='Custom',
    config={{
        'prefix': Text('Prefix', description='Prepended to the incoming value.', default=''),
    }},
    inputs={{
        'value': Text('Value', sensitive=True),
    }},
    outputs={{
        'value': Text('Value', sensitive=True),
    }},
)
async def execute(ctx):
    return {{'value': ctx.config['prefix'] + ctx.inputs['value']}}
'''


def packages(root):
    directory = Path(root)
    if not directory.is_dir():
        raise ArtifactError(f'{root} is not a directory')
    return sorted(path for path in directory.iterdir() if (path / 'plugin.py').is_file())


def command_new(args):
    if not IDENTIFIER.fullmatch(args.name) or '.' not in args.name:
        raise SystemExit('Name must be a lowercase namespaced identifier such as team.customer-lookup')
    directory = Path(args.root) / args.name
    directory.mkdir(parents=True, exist_ok=False)
    title = args.name.split('.', 1)[1].replace('-', ' ').replace('_', ' ').capitalize()
    (directory / 'plugin.py').write_text(SCAFFOLD.format(name=args.name, title=title))
    manifest = build(directory)
    print(f'{directory}  {manifest["name"]}@{manifest["version"]}')
    print('Edit plugin.py, then run: workflow-plugin build')


def command_build(args):
    changed = 0
    for directory in packages(args.root):
        path = directory / 'manifest.json'
        before = path.read_text() if path.exists() else None
        manifest = build(directory)
        if before is None:
            state = 'created'
        elif before != path.read_text():
            state = 'updated'
        else:
            state = 'unchanged'
        changed += state != 'unchanged'
        print(f'{state:>9}  {manifest["name"]}@{manifest["version"]}  {manifest["digest"][:12]}')
    if changed:
        print(f'\n{changed} manifest(s) regenerated. Restart the platform and worker to register them.')


def command_validate(args):
    problems = []
    installed = {}
    for directory in packages(args.root):
        try:
            manifest = read(directory)
            verify(directory, manifest)
            expected = dict(declared(directory))
            expected['digest'] = digest(directory)
            if expected != manifest:
                problems.append(f'{directory.name}: manifest.json is stale. Run "workflow-plugin build".')
                continue
            key = (manifest['name'], manifest['version'])
            if key in installed:
                problems.append(f'{directory.name}: {key[0]}@{key[1]} is already provided by {installed[key]}')
                continue
            installed[key] = directory.name
            print(f'       ok  {manifest["name"]}@{manifest["version"]}  {manifest["digest"][:12]}')
        except (ArtifactError, ValueError) as error:
            problems.append(f'{directory.name}: {error}')
    for problem in problems:
        print(f'    FAILED  {problem}', file=sys.stderr)
    if problems:
        raise SystemExit(1)
    print(f'\n{len(installed)} plugin(s) valid and sealed.')


def command_list(args):
    print(json.dumps([manifest for manifest, _ in catalog(args.root).values()], indent=2))


def command_check(args):
    stale = drift(args.root)
    if stale:
        print('Stale manifests: ' + ', '.join(stale), file=sys.stderr)
        raise SystemExit(1)
    print('All manifests match their declarations.')


def command_import_openapi(args):
    from .openapi import import_operation
    print(import_operation(args.document, args.operation, args.name, args.base_url, args.root))


def main():
    parser = argparse.ArgumentParser(
        prog='workflow-plugin',
        description='Create, build, and verify workflow plugin packages.',
    )
    sub = parser.add_subparsers(dest='command', required=True)

    def with_root(command, help_text):
        item = sub.add_parser(command, help=help_text)
        item.add_argument('--root', default='plugins')
        return item

    create = sub.add_parser('new', help='Scaffold a new plugin package')
    create.add_argument('name')
    create.add_argument('--root', default='plugins')
    create.set_defaults(handler=command_new)

    with_root('build', 'Generate manifest.json and seal each package').set_defaults(handler=command_build)
    with_root('validate', 'Verify every manifest matches its code and its files').set_defaults(handler=command_validate)
    with_root('check', 'Exit non-zero when any manifest is stale').set_defaults(handler=command_check)
    with_root('list', 'Print the installed catalog as JSON').set_defaults(handler=command_list)
    # Retained so existing docs and scripts keep working.
    with_root('seal', argparse.SUPPRESS).set_defaults(handler=command_build)

    imported = sub.add_parser('import-openapi', help='Generate a plugin from one OpenAPI operation')
    imported.add_argument('document')
    imported.add_argument('--operation', required=True)
    imported.add_argument('--name', required=True)
    imported.add_argument('--base-url', required=True)
    imported.add_argument('--root', default='plugins')
    imported.set_defaults(handler=command_import_openapi)

    args = parser.parse_args()
    try:
        args.handler(args)
    except ArtifactError as error:
        raise SystemExit(str(error))


if __name__ == '__main__':
    main()
