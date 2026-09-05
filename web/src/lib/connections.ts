import type { Manifest, Port } from './types';
export function compatible(output: Port, input: Port): boolean {
  return (
    output.kind === input.kind &&
    output.resourceType === input.resourceType &&
    (!output.sensitive || !!input.sensitive) &&
    (!output.schema.type ||
      !input.schema.type ||
      output.schema.type === input.schema.type)
  );
}
export function latestPlugins(catalog: Record<string, Manifest>): Manifest[] {
  const latest = new Map<string, Manifest>();
  for (const m of Object.values(catalog)) {
    const current = latest.get(m.name);
    if (
      !current ||
      m.version.localeCompare(current.version, undefined, { numeric: true }) > 0
    )
      latest.set(m.name, m);
  }
  return [...latest.values()].sort((a, b) => a.title.localeCompare(b.title));
}

export function portSide(port: Port, direction: 'input' | 'output') {
  return port.side ?? (port.kind === 'resource'
    ? direction === 'input' ? 'bottom' : 'top'
    : direction === 'input' ? 'left' : 'right');
}
