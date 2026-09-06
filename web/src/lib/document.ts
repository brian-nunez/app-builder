import { useCallback, useEffect, useRef, useState } from 'react';
import { applyEdgeChanges, applyNodeChanges } from '@xyflow/react';
import type { Edge, EdgeChange, NodeChange } from '@xyflow/react';
import { api } from './api';
import { latestPlugins } from './connections';
import type { Graph, Manifest, Workflow, WorkflowNode as Definition } from './types';
import type { CanvasNode } from '../components/WorkflowNode';

export type SaveStatus = 'saved' | 'unsaved' | 'saving' | 'failed' | 'upgrading';

export const SAVE_LABEL: Record<SaveStatus, string> = {
  saved: 'Saved',
  unsaved: 'Unsaved changes',
  saving: 'Saving…',
  failed: 'Save failed',
  upgrading: 'Upgrading plugins…',
};

export const AUTOSAVE_INTERVAL = 1200;

const UPGRADE_NOTICE =
  'Older plugin versions were upgraded to the installed versions. The workflow will be saved as a new revision.';

/** Starting configuration for a new node, from the schema's declared defaults. */
export function configDefaults(manifest: Manifest): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(manifest.configSchema.properties ?? {}).map(([key, value]) => [
      key,
      value.default ??
        (value.type === 'string'
          ? ''
          : value.type === 'boolean'
            ? false
            : value.type === 'object'
              ? {}
              : value.type === 'array'
                ? []
                : null),
    ]),
  );
}

/** A stand-in so a node whose plugin was uninstalled still renders and can be removed. */
export function unavailableManifest(definition: Definition): Manifest {
  return {
    protocol: 'workflow.plugin/v1',
    name: definition.plugin,
    version: definition.version,
    title: 'Unavailable plugin',
    description: 'This plugin is not installed. Replace or remove this node.',
    category: 'Unavailable',
    kind: 'action',
    configSchema: { type: 'object', properties: {} },
    inputs: [],
    outputs: [],
    permissions: [],
    digest: '',
  };
}

/** The canvas's node and edge state, in the shape the API stores. */
export function toGraph(nodes: CanvasNode[], edges: Edge[]): Graph {
  return {
    nodes: nodes.map((n) => ({ ...n.data.definition, position: n.position })),
    edges: edges.map((e) => ({
      id: e.id,
      source: e.source,
      sourcePort: e.sourceHandle ?? '',
      target: e.target,
      targetPort: e.targetHandle ?? '',
    })),
  };
}

/**
 * The open workflow: its graph, its save state, and the rules that keep the two
 * consistent.
 *
 * Everything the editor needs to know about persistence lives here — the
 * autosave cadence, the optimistic-concurrency conflict, the guard against a
 * stale response overwriting a newer edit, and the plugin-version upgrade applied
 * when a saved graph pins releases that are no longer installed. Callers see a
 * graph, a status, and three verbs.
 */
export function useWorkflowDocument(
  catalog: Record<string, Manifest>,
  onError: (error: unknown) => void,
) {
  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [nodes, setNodes] = useState<CanvasNode[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [status, setStatus] = useState<SaveStatus>('saved');
  const [notice, setNotice] = useState('');

  const dirty = useRef(false);
  const saving = useRef(false);
  const blocked = useRef(false);
  // Bumped whenever a different workflow is opened, so a save that is still in
  // flight cannot apply its response to the document that replaced it.
  const generation = useRef(0);
  const current = useRef<Workflow | null>(null);
  const graph = useRef({ nodes, edges });

  const open = useCallback(
    (item: Workflow) => {
      generation.current++;
      blocked.current = false;
      current.current = item;
      setWorkflow(item);
      let upgraded = false;
      const installed = latestPlugins(catalog);
      const nextNodes: CanvasNode[] = (item.graph.nodes ?? []).map((definition) => {
        const exact = catalog[`${definition.plugin}@${definition.version}`];
        const replacement =
          exact ?? installed.find((manifest) => manifest.name === definition.plugin);
        const manifest = replacement ?? unavailableManifest(definition);
        const next =
          replacement && !exact
            ? {
                ...definition,
                version: replacement.version,
                config: { ...configDefaults(replacement), ...definition.config },
              }
            : definition;
        if (replacement && !exact) upgraded = true;
        return {
          id: next.id,
          type: 'plugin',
          position: next.position,
          data: { definition: next, manifest },
        };
      });
      const nextEdges = (item.graph.edges ?? []).map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        sourceHandle: e.sourcePort,
        targetHandle: e.targetPort,
        type: 'smoothstep',
      }));
      graph.current = { nodes: nextNodes, edges: nextEdges };
      dirty.current = upgraded;
      setNodes(nextNodes);
      setEdges(nextEdges);
      setStatus(upgraded ? 'upgrading' : 'saved');
      setNotice(upgraded ? UPGRADE_NOTICE : '');
    },
    [catalog],
  );

  const close = useCallback(() => {
    generation.current++;
    dirty.current = false;
    blocked.current = false;
    current.current = null;
    graph.current = { nodes: [], edges: [] };
    setWorkflow(null);
    setNodes([]);
    setEdges([]);
    setStatus('saved');
    setNotice('');
  }, []);

  /** Record a change that must reach the server. */
  const apply = useCallback((nextNodes: CanvasNode[], nextEdges: Edge[]) => {
    graph.current = { nodes: nextNodes, edges: nextEdges };
    setNodes(nextNodes);
    setEdges(nextEdges);
    dirty.current = true;
    blocked.current = false;
    setStatus('unsaved');
  }, []);

  const save = useCallback(async () => {
    const item = current.current;
    if (!item || !dirty.current || saving.current || blocked.current) return;
    saving.current = true;
    dirty.current = false;
    setStatus('saving');
    const version = generation.current;
    try {
      const saved = await api<Workflow>(`/api/workflows/${item.id}`, 'PUT', {
        name: item.name,
        base: item.head,
        graph: toGraph(graph.current.nodes, graph.current.edges),
        message: 'Updated workflow',
      });
      if (generation.current === version) {
        current.current = saved;
        setWorkflow(saved);
        setStatus(dirty.current ? 'unsaved' : 'saved');
        setNotice('');
      }
    } catch (error) {
      if (generation.current === version) {
        dirty.current = true;
        // Stop retrying on a rejected save so a conflict is not hammered every
        // interval; the next edit clears the block.
        blocked.current = true;
        setStatus('failed');
        onError(error);
      }
    } finally {
      saving.current = false;
    }
  }, [onError]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (dirty.current && !saving.current) void save();
    }, AUTOSAVE_INTERVAL);
    return () => window.clearInterval(timer);
  }, [save]);

  useEffect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (dirty.current || saving.current) event.preventDefault();
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, []);

  const onNodesChange = useCallback(
    (changes: NodeChange<CanvasNode>[]) => {
      const next = applyNodeChanges(changes, graph.current.nodes);
      // Selection and dimension changes are presentation only; they must not
      // mark the document dirty or the editor would save on every click.
      if (changes.some((c) => c.type === 'remove' || c.type === 'position')) {
        apply(
          next,
          graph.current.edges.filter(
            (e) => next.some((n) => n.id === e.source) && next.some((n) => n.id === e.target),
          ),
        );
      } else {
        graph.current.nodes = next;
        setNodes(next);
      }
    },
    [apply],
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      const next = applyEdgeChanges(changes, graph.current.edges);
      if (changes.some((c) => c.type === 'remove')) {
        apply(graph.current.nodes, next);
      } else {
        graph.current.edges = next;
        setEdges(next);
      }
    },
    [apply],
  );

  /** Whether every edit has reached the server, so leaving loses nothing. */
  const settled = useCallback(() => !dirty.current && !saving.current, []);

  /** Take a workflow record the server returned without disturbing the graph. */
  const adopt = useCallback((item: Workflow) => {
    current.current = item;
    setWorkflow(item);
  }, []);

  const rename = useCallback((next: string) => {
    const item = current.current;
    if (!item) return;
    const renamed = { ...item, name: next };
    current.current = renamed;
    setWorkflow(renamed);
    dirty.current = true;
    blocked.current = false;
    setStatus('unsaved');
  }, []);

  return {
    workflow,
    nodes,
    edges,
    status,
    label: SAVE_LABEL[status],
    notice,
    /** The live graph, for handlers that must read it outside a render. */
    graph,
    open,
    close,
    apply,
    save,
    settled,
    adopt,
    rename,
    onNodesChange,
    onEdgesChange,
  };
}
