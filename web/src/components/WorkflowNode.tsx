import { useEffect } from 'react';
import { Handle, Position, useUpdateNodeInternals } from '@xyflow/react';
import type { NodeProps, Node } from '@xyflow/react';
import { Box, Cable, Zap } from 'lucide-react';
import type { Manifest, WorkflowNode as Definition } from '../lib/types';
import { portSide } from '../lib/connections';

export type CanvasNode = Node<
  { definition: Definition; manifest: Manifest },
  'plugin'
>;
export function WorkflowNode({ id, data, selected }: NodeProps<CanvasNode>) {
  const { definition, manifest } = data;
  const updateInternals = useUpdateNodeInternals();
  const ports = [
    ...manifest.inputs.map((port) => ({ ...port, direction: 'input' as const })),
    ...manifest.outputs.map((port) => ({ ...port, direction: 'output' as const })),
  ].map((port) => ({ ...port, side: portSide(port, port.direction) }));
  const onSide = (side: string) => ports.filter((port) => port.side === side);
  const topHeight = onSide('top').length ? 48 : 0;
  const bodyHeight = Math.max(onSide('left').length, onSide('right').length, 1) * 34 + 16;
  const width = Math.max(320, Math.max(onSide('top').length, onSide('bottom').length) * 110);
  const signature = ports.map((p) => `${p.direction}:${p.name}:${p.side}`).join('|');
  useEffect(() => { updateInternals(id); }, [id, signature, updateInternals]);
  const Icon = manifest.kind === 'trigger' ? Zap : manifest.kind === 'resource' ? Cable : Box;
  return (
    <div className={`workflow-node directional-node ${selected ? 'selected' : ''}`} style={{ width }}>
      {!!topHeight && <div className="port-rail" style={{ height: topHeight }} />}
      <div className="node-title">
        <span className={`node-icon ${manifest.kind}`}><Icon size={17} /></span>
        <div><strong>{definition.name}</strong><small>{manifest.title}</small></div>
        <span className="node-version">{manifest.version}</span>
      </div>
      <div style={{ height: bodyHeight }} />
      <div className="node-footer"><span className={`kind-dot ${manifest.kind}`} />{manifest.kind}<span>{manifest.category}</span></div>
      {!!onSide('bottom').length && <div className="port-rail" style={{ height: 48 }} />}
      {ports.map((port) => {
        const group = onSide(port.side);
        const index = group.indexOf(port);
        const horizontal = port.side === 'top' || port.side === 'bottom';
        const offset = horizontal ? `${(index + 0.5) * 100 / group.length}%` : topHeight + 64 + index * 34 + 24;
        const position = { left: Position.Left, right: Position.Right, top: Position.Top, bottom: Position.Bottom }[port.side];
        return (
          <div key={`${port.direction}:${port.name}`}>
            <Handle
              type={port.direction === 'input' ? 'target' : 'source'}
              position={position}
              id={port.name}
              className={`directional-handle ${port.kind}`}
              style={horizontal ? { left: offset } : { top: offset }}
              aria-label={`${definition.name}: ${port.title} ${port.direction}`}
              title={`${port.title} · ${port.direction === 'input' ? 'Input' : 'Output'}${port.multiple ? ' · Multiple connections' : ''}`}
            />
            <span className={`directional-label side-${port.side}`} style={horizontal ? { left: offset } : { top: offset }}>
              <span className="port-direction">{port.direction === 'input' ? 'IN' : 'OUT'}</span>
              <span>{port.title}{port.required && <span className="required">*</span>}{port.multiple && ' +'}</span>
            </span>
          </div>
        );
      })}
    </div>
  );
}
