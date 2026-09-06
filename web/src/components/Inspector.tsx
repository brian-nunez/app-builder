import { useMemo, useState } from 'react';
import {
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  Code2,
  Copy,
  Settings2,
  SlidersHorizontal,
  Trash2,
  Unplug,
  X,
} from 'lucide-react';
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

const labelFor = (name: string, schema: Schema) =>
  schema.title ?? name.replaceAll('_', ' ').replace(/^./, (c) => c.toUpperCase());

function JsonField({ name, value, onChange }: { name: string; value: unknown; onChange: (value: unknown) => void }) {
  const [text, setText] = useState(() => JSON.stringify(value ?? {}, null, 2));
  const [error, setError] = useState('');
  return (
    <>
      <textarea
        aria-label={name}
        className="code-input"
        rows={7}
        spellCheck={false}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          try {
            const parsed: unknown = JSON.parse(event.target.value);
            setError('');
            onChange(parsed);
          } catch {
            setError('This value must be valid JSON.');
          }
        }}
      />
      {error && <small className="field-error">{error}</small>}
    </>
  );
}

function ValueField({ name, schema, value, onChange }: { name: string; schema: Schema; value: unknown; onChange: (value: unknown) => void }) {
  if (!schema.type && !schema.enum) return <JsonField key={JSON.stringify(value)} name={name} value={value ?? null} onChange={onChange} />;
  if (schema.enum) {
    return <select value={String(value ?? '')} onChange={(e) => onChange(e.target.value)}>{schema.enum.map((option) => <option key={option}>{option}</option>)}</select>;
  }
  if (schema.type === 'boolean') {
    return (
      <button type="button" role="switch" aria-checked={value === true} className={`toggle-control ${value === true ? 'active' : ''}`} onClick={() => onChange(value !== true)}>
        <span /> {value === true ? 'Enabled' : 'Disabled'}
      </button>
    );
  }
  if (schema.type === 'number' || schema.type === 'integer') {
    return <input type="number" aria-label={name} min={schema.minimum} max={schema.maximum} step={schema.type === 'integer' ? 1 : 'any'} value={typeof value === 'number' ? value : ''} onChange={(e) => onChange(e.target.value === '' ? null : Number(e.target.value))} />;
  }
  if (schema.type === 'array') {
    const items = Array.isArray(value) ? value : [];
    if (schema.items?.enum) {
      return (
        <div className="choice-grid">
          {schema.items.enum.map((option) => (
            <label key={option} className={items.includes(option) ? 'selected' : ''}>
              <input type="checkbox" checked={items.includes(option)} onChange={(e) => onChange(e.target.checked ? [...items, option] : items.filter((item) => item !== option))} />
              {option}
            </label>
          ))}
        </div>
      );
    }
    return <textarea aria-label={name} rows={4} placeholder="One value per line" value={items.join('\n')} onChange={(e) => onChange(e.target.value.split('\n').map((item) => item.trim()).filter(Boolean))} />;
  }
  if (schema.type === 'string') {
    const multiline = /prompt|document|template|body|query/i.test(name);
    const shared = {
      'aria-label': name,
      value: String(value ?? ''),
      onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(e.target.value),
    };
    if (schema.writeOnly) return <input {...shared} type="password" autoComplete="new-password" />;
    return multiline ? <textarea {...shared} rows={5} /> : <input {...shared} type={schema.format === 'uri' ? 'url' : 'text'} />;
  }
  return <JsonField key={JSON.stringify(value)} name={name} value={value} onChange={onChange} />;
}

export function Inspector({ node, manifest, onChange, onDelete, onClose, bindings, workflowId, saved, published, onRunReceived, onVersion, onDuplicate }: Props) {
  const [tab, setTab] = useState<'inputs' | 'settings' | 'advanced'>('inputs');
  const [confirmRemove, setConfirmRemove] = useState(false);
  const required = useMemo(() => manifest.configSchema.required ?? [], [manifest.configSchema.required]);
  const missing = useMemo(() => required.filter((key) => [null, undefined, ''].includes(node.config[key] as null | undefined | string)), [node.config, required]);
  const incoming = bindings.edges.filter((edge) => edge.target === node.id).length;
  const missingInputs = manifest.inputs.filter((input) => input.required && !(manifest.kind === 'trigger' && input.name === 'event') && !bindings.edges.some((edge) => edge.target === node.id && edge.targetHandle === input.name));
  const issueCount = missing.length + missingInputs.length;

  return (
    <aside className="inspector" aria-label={`${node.name} settings`}>
      <div className="inspector-header">
        <div className={`node-symbol ${manifest.kind}`}><Settings2 size={18} /></div>
        <div><span>{manifest.kind} · {manifest.category}</span><h2>{node.name}</h2></div>
        <button className="icon-button" aria-label="Close inspector" onClick={onClose}><X size={18} /></button>
      </div>
      <div className={`configuration-health ${issueCount ? 'warning' : ''}`}>
        {issueCount ? <CircleAlert size={15} /> : <CheckCircle2 size={15} />}
        <span>{issueCount ? `${issueCount} required item${issueCount === 1 ? '' : 's'} missing` : 'Configuration ready'}</span>
        <small>{incoming} input{incoming === 1 ? '' : 's'} connected</small>
      </div>
      <div className="tabs inspector-tabs" role="tablist">
        <button className={tab === 'inputs' ? 'active' : ''} onClick={() => setTab('inputs')}><Unplug size={14} />Inputs</button>
        <button className={tab === 'settings' ? 'active' : ''} onClick={() => setTab('settings')}><SlidersHorizontal size={14} />Settings</button>
        <button className={tab === 'advanced' ? 'active' : ''} onClick={() => setTab('advanced')}><Code2 size={14} />Advanced</button>
      </div>
      <div className="inspector-body">
        {tab === 'inputs' && (
          <>
            <div className="section-copy"><h3>Connect this node</h3><p>Select an existing output or create a compatible source in place.</p></div>
            <InputBindings {...bindings} nodeId={node.id} manifest={manifest} />
            {!manifest.inputs.length && <div className="quiet-state"><CheckCircle2 size={18} /><span>This node starts a branch and does not need inputs.</span></div>}
            {bindings.edges.some((edge) => edge.source === node.id) && (
              <details className="settings-group" open>
                <summary>Used by <ChevronDown size={14} /></summary>
                <div className="linked-nodes">
                  {[...new Set(bindings.edges.filter((e) => e.source === node.id).map((e) => e.target))].map((id) => (
                    <button type="button" key={id} onClick={() => bindings.onSelect(id)}>{bindings.nodes.find((n) => n.id === id)?.data.definition.name ?? id}<span>Open →</span></button>
                  ))}
                </div>
              </details>
            )}
            {manifest.kind === 'trigger' && <WebhookSettings workflowId={workflowId} nodeId={node.id} saved={saved} published={published} onRunReceived={onRunReceived} />}
          </>
        )}
        {tab === 'settings' && (
          <>
            <div className="section-copy"><h3>Node details</h3><p>Clear labels make larger workflows easier to scan.</p></div>
            <label className="field"><span>Node name</span><input value={node.name} maxLength={120} onChange={(e) => onChange({ ...node, name: e.target.value })} /></label>
            {manifest.outputs.some((port) => port.kind === 'data') && (
              <label className="field">
                <span>Workflow response</span>
                <select value={node.responsePort ?? ''} onChange={(e) => onChange({ ...node, responsePort: e.target.value })}>
                  <option value="">Not returned to webhook callers</option>
                  {manifest.outputs.filter((port) => port.kind === 'data').map((port) => <option key={port.name} value={port.name}>{port.title}</option>)}
                </select>
                <small>Expose one output when a webhook waits for this workflow.</small>
              </label>
            )}
            {!!Object.keys(manifest.configSchema.properties ?? {}).length && (
              <details className="settings-group" open>
                <summary>Plugin settings <ChevronDown size={14} /></summary>
                <div className="settings-fields">
                  {Object.entries(manifest.configSchema.properties ?? {}).map(([name, schema]) => (
                    <label className="field" key={name}>
                      <span>{labelFor(name, schema)}{required.includes(name) && <em>Required</em>}</span>
                      <ValueField name={name} schema={schema} value={node.config[name]} onChange={(value) => onChange({ ...node, config: { ...node.config, [name]: value } })} />
                      {schema.description && <small>{schema.description}</small>}
                    </label>
                  ))}
                </div>
              </details>
            )}
          </>
        )}
        {tab === 'advanced' && (
          <>
            <div className="plugin-summary"><span>PLUGIN</span><strong>{manifest.title}</strong><p>{manifest.description}</p><code>{manifest.name}@{manifest.version}</code></div>
            <label className="field"><span>Plugin version</span><select value={manifest.version} onChange={(e) => onVersion(e.target.value)}>{Object.values(bindings.catalog).filter((item) => item.name === manifest.name).sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true })).map((item) => <option key={item.version}>{item.version}</option>)}</select></label>
            <details className="settings-group">
              <summary>Observability <ChevronDown size={14} /></summary>
              <div className="settings-fields">
                <p className="helper-copy">Choose payload fields that are safe to include in execution logs.</p>
                {manifest.logFields?.length ? manifest.logFields.map((field) => (
                  <label className="check-row" key={field}><input type="checkbox" checked={node.logFields?.includes(field) ?? false} onChange={(e) => onChange({ ...node, logFields: e.target.checked ? [...(node.logFields ?? []), field] : node.logFields?.filter((value) => value !== field) })} />Log {field.replaceAll('_', ' ')}</label>
                )) : <small>No optional payload fields are declared.</small>}
              </div>
            </details>
            <details className="settings-group contract-group"><summary>Plugin contract <ChevronDown size={14} /></summary><pre>{JSON.stringify(manifest, null, 2)}</pre></details>
          </>
        )}
      </div>
      <div className="inspector-bottom">
        {confirmRemove ? (
          <div className="inline-confirm"><span>Remove node and connections?</span><button className="button danger-button" onClick={onDelete}>Remove</button><button className="button" onClick={() => setConfirmRemove(false)}>Cancel</button></div>
        ) : (
          <><button className="text-button danger" onClick={() => setConfirmRemove(true)}><Trash2 size={15} />Remove</button><button className="text-button" onClick={onDuplicate}><Copy size={15} />Duplicate</button><span>v{manifest.version}</span></>
        )}
      </div>
    </aside>
  );
}
