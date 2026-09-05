import { compatible, latestPlugins } from '../lib/connections';
import type { Manifest } from '../lib/types';
import type { CanvasNode } from './WorkflowNode';
import type { Edge } from '@xyflow/react';

export type BindingProps = {
  nodeId: string;
  manifest: Manifest;
  nodes: CanvasNode[];
  edges: Edge[];
  catalog: Record<string, Manifest>;
  onBind: (input: string, source: string, output: string) => void;
  onUnbind: (edgeId: string) => void;
  onAddSource: (input: string, manifest: Manifest, output: string) => void;
  onSelect: (nodeId: string) => void;
};
export function InputBindings({
  nodeId,
  manifest,
  nodes,
  edges,
  catalog,
  onBind,
  onUnbind,
  onAddSource,
  onSelect,
}: BindingProps) {
  if (!manifest.inputs.length) return null;
  return (
    <section className="input-bindings">
      <h3>Inputs & resources</h3>
      <p>
        Choose an existing node or add a compatible component. Each input has
        its own connection.
      </p>
      {manifest.inputs.map((input) => {
        const connections = edges.filter(
          (e) => e.target === nodeId && e.targetHandle === input.name,
        );
        const sources = nodes
          .filter((n) => n.id !== nodeId)
          .flatMap((n) =>
            n.data.manifest.outputs
              .filter((p) => compatible(p, input))
              .map((p) => ({ node: n, port: p })),
          );
        const providers = latestPlugins(catalog).flatMap((m) =>
          m.outputs
            .filter((p) => compatible(p, input))
            .map((p) => ({ manifest: m, port: p })),
        );
        const suppliedByTrigger =
          manifest.kind === 'trigger' && input.name === 'event';
        return (
          <div className="input-binding" key={input.name}>
            <div className="input-heading">
              <label htmlFor={`bind-${nodeId}-${input.name}`}>
                {input.title}
                {input.required ? ' *' : ' (optional)'}
              </label>
              <span>
                {input.multiple
                  ? 'Multiple sources'
                  : input.kind === 'resource'
                    ? 'Resource'
                    : 'Value'}
              </span>
            </div>
            {suppliedByTrigger ? (
              <p>Provided by incoming requests or the Run input.</p>
            ) : (
              <>
                <select
                  id={`bind-${nodeId}-${input.name}`}
                  value={
                    !input.multiple && connections[0]
                      ? `${connections[0].source}|${connections[0].sourceHandle}`
                      : ''
                  }
                  onChange={(e) => {
                    if (!e.target.value) {
                      connections.forEach((edge) => onUnbind(edge.id));
                      return;
                    }
                    const parts = e.target.value.split('|');
                    if (parts[0] === 'new')
                      onAddSource(input.name, catalog[parts[1]], parts[2]);
                    else onBind(input.name, parts[0], parts[1]);
                  }}
                >
                  <option value="">
                    {input.multiple
                      ? 'Add a source…'
                      : `Select ${input.title.toLowerCase()}…`}
                  </option>
                  {sources.length > 0 && (
                    <optgroup label="Existing nodes">
                      {sources.map(({ node, port }) => (
                        <option
                          key={node.id + port.name}
                          value={`${node.id}|${port.name}`}
                          disabled={
                            !!input.multiple &&
                            connections.some(
                              (e) =>
                                e.source === node.id &&
                                e.sourceHandle === port.name,
                            )
                          }
                        >
                          {node.data.definition.name} → {port.title}
                        </option>
                      ))}
                    </optgroup>
                  )}
                  <optgroup label="Add a new component">
                    {providers.map(({ manifest: m, port }) => (
                      <option
                        key={m.name + port.name}
                        value={`new|${m.name}@${m.version}|${port.name}`}
                      >
                        + {m.title} → {port.title}
                      </option>
                    ))}
                  </optgroup>
                </select>
                {connections.map((edge) => (
                  <div className="binding-source" key={edge.id}>
                    <button type="button" onClick={() => onSelect(edge.source)}>
                      Configure{' '}
                      {nodes.find((n) => n.id === edge.source)?.data.definition
                        .name ?? 'source'}
                    </button>
                    <button
                      type="button"
                      aria-label={`Disconnect ${input.title} from ${nodes.find((n) => n.id === edge.source)?.data.definition.name}`}
                      onClick={() => onUnbind(edge.id)}
                    >
                      Disconnect
                    </button>
                  </div>
                ))}
                {input.required && connections.length === 0 && (
                  <small>Required before this workflow can run.</small>
                )}
              </>
            )}
          </div>
        );
      })}
    </section>
  );
}
