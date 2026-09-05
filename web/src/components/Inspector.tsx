import { useState } from 'react';
import { Code2, Radio, Trash2, X, Copy } from 'lucide-react';
import type { Manifest, Schema, WorkflowNode } from '../lib/types';
import { InputBindings } from './InputBindings';
import type { BindingProps } from './InputBindings';
import { WebhookSettings } from './WebhookSettings';

type Props = {
  node: WorkflowNode;
  manifest: Manifest;
  onChange: (node: WorkflowNode) => void;
  onDelete: () => void;
  onClose: () => void;
  bindings: Omit<BindingProps, 'nodeId' | 'manifest'>;
  workflowId: string;
  saved: boolean;
  published: number | null;
  onRunReceived: (id: string) => void;
  onVersion: (version: string) => void;
  onDuplicate: () => void;
};
function ValueField({
  name,
  schema,
  value,
  onChange,
}: {
  name: string;
  schema: Schema;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const [text, setText] = useState(() =>
    JSON.stringify(value ?? schema.default ?? null, null, 2),
  );
  const [error, setError] = useState('');
  if (!schema.type && !schema.enum) {
    const kind = Array.isArray(value)
      ? 'array'
      : value === null
        ? 'null'
        : typeof value === 'object'
          ? 'object'
          : typeof value;
    const types: Record<string, unknown> = {
      string: '',
      number: 0,
      boolean: false,
      object: {},
      array: [],
      null: null,
    };
    return (
      <div className="typed-value">
        <label>
          Value type
          <select
            aria-label={`${name} type`}
            value={kind in types ? kind : 'object'}
            onChange={(e) => onChange(types[e.target.value])}
          >
            <option value="string">Text</option>
            <option value="number">Number</option>
            <option value="boolean">Yes / no</option>
            <option value="object">JSON object</option>
            <option value="array">JSON array</option>
            <option value="null">Null</option>
          </select>
        </label>
        {kind !== 'null' && (
          <ValueField
            key={kind}
            name={name}
            schema={{ type: kind }}
            value={value}
            onChange={onChange}
          />
        )}
      </div>
    );
  }
  if (schema.enum)
    return (
      <select
        value={String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
      >
        {schema.enum.map((v) => (
          <option key={v}>{v}</option>
        ))}
      </select>
    );
  if (schema.type === 'boolean')
    return (
      <label className="switch-row">
        <input
          type="checkbox"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
        Enabled
      </label>
    );
  if (schema.type === 'number' || schema.type === 'integer')
    return (
      <input
        type="number"
        aria-label={name}
        min={schema.minimum}
        max={schema.maximum}
        step={schema.type === 'integer' ? 1 : 'any'}
        value={typeof value === 'number' ? value : ''}
        onChange={(e) => {
          if (e.target.value !== '') onChange(Number(e.target.value));
        }}
      />
    );
  if (schema.writeOnly)
    return (
      <input
        type="password"
        autoComplete="new-password"
        aria-label={name}
        value={String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  if (schema.type === 'string')
    return (
      <textarea
        aria-label={name}
        rows={name.includes('prompt') ? 5 : 2}
        value={String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  return (
    <>
      <textarea
        aria-label={name}
        className="code-input"
        rows={8}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          try {
            const parsed: unknown = JSON.parse(e.target.value);
            setError('');
            onChange(parsed);
          } catch {
            setError('Enter valid JSON to save this field.');
          }
        }}
      />
      {error && <small className="field-error">{error}</small>}
    </>
  );
}
export function Inspector({
  node,
  manifest,
  onChange,
  onDelete,
  onClose,
  bindings,
  workflowId,
  saved,
  published,
  onRunReceived,
  onVersion,
  onDuplicate,
}: Props) {
  const [tab, setTab] = useState<'config' | 'contract'>('config');
  return (
    <aside className="inspector">
      <div className="panel-heading">
        <span>Node settings</span>
        <button
          className="icon-button"
          aria-label="Close inspector"
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      <div className="inspector-intro">
        <span className="eyebrow">{manifest.kind} plugin</span>
        <h2>{manifest.title}</h2>
        <p>{manifest.description}</p>
        <code>
          {manifest.name}@{manifest.version}
        </code>
      </div>
      <div className="tabs">
        <button
          className={tab === 'config' ? 'active' : ''}
          onClick={() => setTab('config')}
        >
          Configuration
        </button>
        <button
          className={tab === 'contract' ? 'active' : ''}
          onClick={() => setTab('contract')}
        >
          <Code2 size={13} />
          Contract
        </button>
      </div>
      <div className="inspector-body">
        {tab === 'config' ? (
          <>
            {bindings.edges.some((e) => e.source === node.id) && (
              <div className="used-by">
                <span>Used by</span>
                {[
                  ...new Set(
                    bindings.edges
                      .filter((e) => e.source === node.id)
                      .map((e) => e.target),
                  ),
                ].map((id) => (
                  <button
                    type="button"
                    key={id}
                    onClick={() => bindings.onSelect(id)}
                  >
                    {bindings.nodes.find((n) => n.id === id)?.data.definition
                      .name ?? id}{' '}
                    →
                  </button>
                ))}
              </div>
            )}
            <InputBindings {...bindings} nodeId={node.id} manifest={manifest} />
            {manifest.kind === 'trigger' && (
              <WebhookSettings
                workflowId={workflowId}
                nodeId={node.id}
                saved={saved}
                published={published}
                onRunReceived={onRunReceived}
              />
            )}
            <h3 className="settings-subheading">Component settings</h3>
            <label className="field">
              Plugin version
              <select
                value={manifest.version}
                onChange={(e) => onVersion(e.target.value)}
              >
                {Object.values(bindings.catalog)
                  .filter((m) => m.name === manifest.name)
                  .sort((a, b) =>
                    b.version.localeCompare(a.version, undefined, {
                      numeric: true,
                    }),
                  )
                  .map((m) => (
                    <option key={m.version} value={m.version}>
                      {m.version}
                      {m.version === manifest.version ? ' (current)' : ''}
                    </option>
                  ))}
              </select>
            </label>
            <label className="field">
              Node name
              <input
                value={node.name}
                onChange={(e) => onChange({ ...node, name: e.target.value })}
              />
            </label>
            {manifest.outputs.some((port) => port.kind === 'data') && (
              <label className="field">
                Webhook response output
                <select
                  value={node.responsePort ?? ''}
                  onChange={(e) => onChange({ ...node, responsePort: e.target.value })}
                >
                  <option value="">Do not return this node</option>
                  {manifest.outputs.filter((port) => port.kind === 'data').map((port) => (
                    <option key={port.name} value={port.name}>{port.title}</option>
                  ))}
                </select>
                <small>Select one output across the workflow to return to the webhook caller. Save and publish to update the live endpoint.</small>
              </label>
            )}
            {Object.entries(manifest.configSchema.properties ?? {}).map(
              ([name, schema]) => (
                <div className="field" key={name}>
                  {schema.title ?? name.replaceAll('_', ' ')}
                  {manifest.configSchema.required?.includes(name) && (
                    <span className="required"> *</span>
                  )}
                  <ValueField
                    name={name}
                    schema={schema}
                    value={node.config[name]}
                    onChange={(value) =>
                      onChange({
                        ...node,
                        config: { ...node.config, [name]: value },
                      })
                    }
                  />
                  {schema.description && <small>{schema.description}</small>}
                </div>
              ),
            )}
            <section className="inspector-section">
              <h3>
                <Radio size={13} /> Observability
              </h3>
              <p>
                Execution logs, traces, and metrics include workflow and plugin
                context automatically.
              </p>
              {manifest.logFields?.length ? (
                manifest.logFields.map((f) => (
                  <label className="switch-row" key={f}>
                    <input
                      type="checkbox"
                      checked={node.logFields?.includes(f) ?? false}
                      onChange={(e) =>
                        onChange({
                          ...node,
                          logFields: e.target.checked
                            ? [...(node.logFields ?? []), f]
                            : node.logFields?.filter((v) => v !== f),
                        })
                      }
                    />
                    Log {f.replaceAll('_', ' ')}
                  </label>
                ))
              ) : (
                <small>No optional payload fields declared.</small>
              )}
            </section>
          </>
        ) : (
          <>
            <h3>Plugin contract</h3>
            <pre>{JSON.stringify(manifest, null, 2)}</pre>
          </>
        )}
      </div>
      <div className="inspector-bottom">
        <button className="text-button danger" onClick={onDelete}>
          <Trash2 size={14} />
          Remove node
        </button>
        <button className="text-button" onClick={onDuplicate}>
          <Copy size={14} />
          Duplicate node
        </button>
        <span>v{manifest.version} pinned</span>
      </div>
    </aside>
  );
}
