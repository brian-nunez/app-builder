import type { Manifest, Port } from './types';

/**
 * Whether an output port may feed an input port.
 *
 * Mirrors plugin.Connectable in the Go platform, which is the authority. Both are
 * held to sdk/contract/port-compatibility.json so the canvas cannot offer a
 * connection the server will reject on save.
 */
export function compatible(output: Port, input: Port): boolean {
  if (output.kind !== input.kind) return false;
  if ((output.resourceType ?? '') !== (input.resourceType ?? '')) return false;
  if (output.sensitive && !input.sensitive) return false;
  const outputType = output.schema?.type;
  const inputType = input.schema?.type;
  if (outputType && inputType && outputType !== inputType) return false;
  return true;
}

/** Whether an input may take one more source, given how many it already has. */
export function acceptsAnother(input: Port, bound: number): boolean {
  return bound === 0 || !!input.multiple;
}

/**
 * The single check behind both ways of drawing an edge: dragging between handles
 * on the canvas, and choosing a source in the inspector.
 */
export function canConnect(output: Port, input: Port, bound: number): boolean {
  return compatible(output, input) && acceptsAnother(input, bound);
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
  return (
    port.side ??
    (port.kind === 'resource'
      ? direction === 'input'
        ? 'bottom'
        : 'top'
      : direction === 'input'
        ? 'left'
        : 'right')
  );
}
