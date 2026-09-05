import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  applyNodeChanges,
  applyEdgeChanges,
  BackgroundVariant,
  MarkerType,
} from '@xyflow/react';
import type {
  ReactFlowInstance,
  Connection,
  Edge,
  NodeChange,
  EdgeChange,
} from '@xyflow/react';
import {
  ArrowLeft,
  ArrowUpRight,
  Box,
  Check,
  ChevronRight,
  Circle,
  Clock3,
  Code2,
  GitBranch,
  History,
  Layers3,
  LoaderCircle,
  LogOut,
  Play,
  Plus,
  Radio,
  Search,
  ShieldCheck,
  Workflow as WorkflowIcon,
  X,
} from 'lucide-react';
import { api, APIError } from './lib/api';
import type {
  Manifest,
  Revision,
  Run,
  Step,
  Workflow,
  WorkflowNode as Definition,
} from './lib/types';
import { WorkflowNode } from './components/WorkflowNode';
import type { CanvasNode } from './components/WorkflowNode';
import { Inspector } from './components/Inspector';
import { compatible, latestPlugins, portSide } from './lib/connections';

const nodeTypes = { plugin: WorkflowNode };
const emptyGraph = { nodes: [], edges: [] };
const relative = (value: string) =>
  new Date(value).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });

function Login({ onLogin }: { onLogin: () => void }) {
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <div className="login-page">
      <div className="login-brand">
        <Layers3 size={28} />
        <span>forma</span>
      </div>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api('/auth/login', 'POST', { token });
            onLogin();
          } catch (error) {
            setError(String(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <span className="eyebrow">WORKFLOW STUDIO</span>
        <h1>
          Your systems.
          <br />
          Working together.
        </h1>
        <p>Sign in to build, connect, and run your workflows.</p>
        <a className="button full" href="/auth/login">
          Continue with your organization <ArrowUpRight size={16} />
        </a>
        <div className="login-divider">or use a local operator token</div>
        <label className="field">
          Access token
          <input
            type="password"
            autoComplete="current-password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            required
          />
        </label>
        {error && (
          <p role="alert" className="field-error">
            {error}
          </p>
        )}
        <button className="button primary full" disabled={busy}>
          {busy ? 'Signing in…' : 'Open workspace'}
          <ChevronRight size={16} />
        </button>
      </form>
      <span className="login-footer">A workspace for connected work.</span>
    </div>
  );
}

export default function App() {
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [catalog, setCatalog] = useState<Record<string, Manifest>>({});
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [page, setPage] = useState<'workflows' | 'components'>('workflows');
  const [nodes, setNodes] = useState<CanvasNode[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('All components');
  const [catalogOpen, setCatalogOpen] = useState(true);
  const [panel, setPanel] = useState<'history' | 'runs' | null>(null);
  const [history, setHistory] = useState<Revision[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [steps, setSteps] = useState<Step[]>([]);
  const [activeRun, setActiveRun] = useState<string | null>(null);
  const [saveState, setSaveState] = useState('Saved');
  const [error, setError] = useState('');
  const [modal, setModal] = useState<'create' | 'run' | null>(null);
  const [name, setName] = useState('');
  const [runInput, setRunInput] = useState('{}');

  const dirty = useRef(false);
  const saveBlocked = useRef(false);
  const saving = useRef(false);
  const generation = useRef(0);
  const workflowRef = useRef<Workflow | null>(null);
  const graphRef = useRef({ nodes, edges });
  const flowRef = useRef<ReactFlowInstance<CanvasNode, Edge> | null>(null);
  const onError = useCallback((error: unknown) => {
    setError(error instanceof Error ? error.message : String(error));
    if (error instanceof APIError && error.status === 401)
      setAuthenticated(false);
  }, []);
  const refresh = useCallback(async () => {
    try {
      const [plugins, list] = await Promise.all([
        api<Record<string, Manifest>>('/api/plugins'),
        api<Workflow[]>('/api/workflows'),
      ]);
      setCatalog(plugins);
      setWorkflows(list);
      setAuthenticated(true);
    } catch (error) {
      onError(error);
    }
  }, [onError]);
  useEffect(() => {
    const task = window.setTimeout(() => void refresh(), 0);
    return () => window.clearTimeout(task);
  }, [refresh]);
  const install = useCallback(
    (item: Workflow) => {
      generation.current++;
      dirty.current = false;
      saveBlocked.current = false;
      workflowRef.current = item;
      setWorkflow(item);
      const nextNodes: CanvasNode[] = (item.graph.nodes ?? []).map(
        (definition) => ({
          id: definition.id,
          type: 'plugin',
          position: definition.position,
          data: {
            definition,
            manifest: catalog[`${definition.plugin}@${definition.version}`],
          },
        }),
      );
      const nextEdges = (item.graph.edges ?? []).map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        sourceHandle: e.sourcePort,
        targetHandle: e.targetPort,
        type: 'smoothstep',
      }));
      graphRef.current = { nodes: nextNodes, edges: nextEdges };
      setNodes(nextNodes);
      setEdges(nextEdges);
      setSelected(null);
      setSaveState('Saved');
      setError('');
    },
    [catalog],
  );
  const save = useCallback(async () => {
    const current = workflowRef.current;
    if (!current || !dirty.current || saving.current || saveBlocked.current)
      return;
    saving.current = true;
    dirty.current = false;
    setSaveState('Saving…');
    const version = generation.current;
    const graph = {
      nodes: graphRef.current.nodes.map((n) => ({
        ...n.data.definition,
        position: n.position,
      })),
      edges: graphRef.current.edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        sourcePort: e.sourceHandle ?? '',
        targetPort: e.targetHandle ?? '',
      })),
    };
    try {
      const saved = await api<Workflow>(`/api/workflows/${current.id}`, 'PUT', {
        name: current.name,
        base: current.head,
        graph,
        message: 'Updated workflow',
      });
      if (generation.current === version) {
        workflowRef.current = saved;
        setWorkflow(saved);
        setSaveState(dirty.current ? 'Unsaved changes' : 'Saved');
      }
    } catch (error) {
      if (generation.current === version) {
        dirty.current = true;
        saveBlocked.current = true;
        setSaveState('Save failed');
        onError(error);
      }
    } finally {
      saving.current = false;
    }
  }, [onError]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (dirty.current && !saving.current) void save();
    }, 1200);
    return () => window.clearInterval(timer);
  }, [save]);
  useEffect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (dirty.current || saving.current) event.preventDefault();
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, []);
  useEffect(() => {
    if (!workflow || !panel) return;
    let stale = false;
    const load = async () => {
      try {
        if (panel === 'history') {
          const result = await api<Revision[]>(
            `/api/workflows/${workflow.id}/history`,
          );
          if (!stale) setHistory(result);
        } else {
          const result = await api<Run[]>(`/api/workflows/${workflow.id}/runs`);
          if (!stale) setRuns(result);
          if (activeRun) {
            const result = await api<Step[]>(`/api/runs/${activeRun}/steps`);
            if (!stale) setSteps(result);
          }
        }
      } catch (error) {
        if (!stale) onError(error);
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 2500);
    return () => {
      stale = true;
      window.clearInterval(timer);
    };
  }, [workflow, panel, activeRun, onError]);
  const changeGraph = (nextNodes: CanvasNode[], nextEdges: Edge[]) => {
    setNodes(nextNodes);
    setEdges(nextEdges);
    graphRef.current = { nodes: nextNodes, edges: nextEdges };
    dirty.current = true;
    saveBlocked.current = false;
    setSaveState('Unsaved changes');
  };
  const onNodesChange = (changes: NodeChange<CanvasNode>[]) => {
    const next = applyNodeChanges(changes, graphRef.current.nodes);
    const substantive = changes.some(
      (c) => c.type === 'remove' || c.type === 'position',
    );
    if (substantive)
      changeGraph(
        next,
        graphRef.current.edges.filter(
          (e) =>
            next.some((n) => n.id === e.source) &&
            next.some((n) => n.id === e.target),
        ),
      );
    else {
      setNodes(next);
      graphRef.current.nodes = next;
    }
  };
  const onEdgesChange = (changes: EdgeChange[]) => {
    const next = applyEdgeChanges(changes, graphRef.current.edges);
    if (changes.some((c) => c.type === 'remove'))
      changeGraph(graphRef.current.nodes, next);
    else {
      setEdges(next);
      graphRef.current.edges = next;
    }
  };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' ||
        target.isContentEditable
      )
        return;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'c') {
        if (selected) {
          const node = graphRef.current.nodes.find((n) => n.id === selected);
          if (node) {
            localStorage.setItem(
              'forma-clipboard',
              JSON.stringify({ ...node.data.definition, type: 'workflow-node' }),
            );
          }
        }
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v') {
        try {
          const data = localStorage.getItem('forma-clipboard');
          if (!data) return;
          const parsed = JSON.parse(data);
          if (parsed.type !== 'workflow-node') return;
          delete parsed.type;
          const definition = parsed as Definition;
          const newId = crypto.randomUUID();
          const position = {
            x: definition.position.x + 30,
            y: definition.position.y + 30,
          };
          const newDef = { ...definition, id: newId, position };
          const manifest = catalog[`${definition.plugin}@${definition.version}`];
          if (!manifest) return;
          changeGraph(
            [
              ...graphRef.current.nodes.map(n => ({ ...n, selected: false })),
              {
                id: newId,
                type: 'plugin',
                position,
                selected: true,
                data: { definition: newDef, manifest },
              },
            ],
            graphRef.current.edges,
          );
          setSelected(newId);
        } catch {
          // ignore parsing errors
        }
      }
    };
    window.addEventListener('keydown', handler, { capture: true });
    return () => window.removeEventListener('keydown', handler, { capture: true });
  }, [selected, catalog]);
  const connect = (c: Connection) => {
    const source = graphRef.current.nodes
      .find((n) => n.id === c.source)
      ?.data.manifest.outputs.find((p) => p.name === c.sourceHandle);
    const input = graphRef.current.nodes
      .find((n) => n.id === c.target)
      ?.data.manifest.inputs.find((p) => p.name === c.targetHandle);
    if (!source || !input || !compatible(source, input)) {
      setError(
        'These ports have incompatible types. Choose a compatible source from Inputs & resources.',
      );
      return;
    }
    const reachable = (
      start: string,
      goal: string,
      seen = new Set<string>(),
    ): boolean => {
      if (start === goal) return true;
      if (seen.has(start)) return false;
      seen.add(start);
      return graphRef.current.edges
        .filter((e) => e.source === start)
        .some((e) => reachable(e.target, goal, seen));
    };
    if (reachable(c.target, c.source)) {
      setError(
        'This connection would create a cycle. Choose an upstream source.',
      );
      return;
    }
    if (
      graphRef.current.edges.some(
        (e) =>
          e.source === c.source &&
          e.sourceHandle === c.sourceHandle &&
          e.target === c.target &&
          e.targetHandle === c.targetHandle,
      )
    )
      return;
    const retained = input.multiple
      ? graphRef.current.edges
      : graphRef.current.edges.filter(
          (e) => e.target !== c.target || e.targetHandle !== c.targetHandle,
        );
    changeGraph(graphRef.current.nodes, [
      ...retained,
      { ...c, id: crypto.randomUUID(), type: 'smoothstep' },
    ]);
  };
  const add = (
    manifest: Manifest,
    target?: { nodeId: string; input: string; output: string },
  ) => {
    const id = crypto.randomUUID();
    const config = Object.fromEntries(
      Object.entries(manifest.configSchema.properties ?? {}).map(
        ([key, value]) => [
          key,
          value.default ??
            (value.type === 'string'
              ? ''
              : value.type === 'boolean'
                ? false
                : value.type === 'object'
                  ? {}
                  : null),
        ],
      ),
    );
    const consumer = target ? graphRef.current.nodes.find((n) => n.id === target.nodeId) : undefined;
    const input = consumer?.data.manifest.inputs.find((p) => p.name === target?.input);
    const side = input ? portSide(input, 'input') : 'left';
    const position = consumer
      ? { x: consumer.position.x + (side === 'left' ? -440 : side === 'right' ? 440 : 0), y: consumer.position.y + (side === 'bottom' ? 380 : side === 'top' ? -380 : 0) }
      : { x: 120 + (nodes.length % 3) * 440, y: 100 + Math.floor(nodes.length / 3) * 380 };
    while (graphRef.current.nodes.some((n) => Math.abs(n.position.x - position.x) < 350 && Math.abs(n.position.y - position.y) < 280)) {
      if (side === 'top' || side === 'bottom') position.x += 380;
      else position.y += 320;
    }
    const definition: Definition = {
      id,
      plugin: manifest.name,
      version: manifest.version,
      name: manifest.title,
      config,
      position,
    };
    changeGraph(
      [
        ...graphRef.current.nodes,
        {
          id,
          type: 'plugin',
          position: definition.position,
          data: { definition, manifest },
        },
      ],
      target
        ? [
            ...graphRef.current.edges.filter(
              (e) =>
                e.target !== target.nodeId ||
                e.targetHandle !== target.input ||
                !!graphRef.current.nodes
                  .find((n) => n.id === target.nodeId)
                  ?.data.manifest.inputs.find((p) => p.name === target.input)
                  ?.multiple,
            ),
            {
              id: crypto.randomUUID(),
              source: id,
              sourceHandle: target.output,
              target: target.nodeId,
              targetHandle: target.input,
              type: 'smoothstep',
            },
          ]
        : graphRef.current.edges,
    );
    setSelected(id);
    requestAnimationFrame(() =>
      requestAnimationFrame(() =>
        flowRef.current?.setCenter(
          definition.position.x + 155,
          definition.position.y + 130,
          { zoom: 1, duration: 200 },
        ),
      ),
    );
  };
  const focusNode = (id: string | null) => {
    setSelected(id);
    const n = graphRef.current.nodes.find((n) => n.id === id);
    if (n)
      void flowRef.current?.setCenter(n.position.x + 155, n.position.y + 130, {
        zoom: 1,
        duration: 200,
      });
  };
  const selectedNode = nodes.find((n) => n.id === selected);
  const manifests = useMemo(() => latestPlugins(catalog), [catalog]);
  const filtered = manifests.filter(
    (m) =>
      (category === 'All components' || m.category === category) &&
      `${m.title} ${m.description} ${m.name}`
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  const canAct = saveState === 'Saved';
  const open = async (id: string) => {
    try {
      install(await api<Workflow>(`/api/workflows/${id}`));
      setPanel(null);
    } catch (error) {
      onError(error);
    }
  };
  const back = async () => {
    await save();
    if (dirty.current || saving.current) return;
    generation.current++;
    workflowRef.current = null;
    setWorkflow(null);
    setQuery('');
    setPanel(null);
    void refresh();
  };
  const restore = async (revision: number, nodeId?: string) => {
    if (!workflow || !canAct) return;
    try {
      install(
        await api<Workflow>(`/api/workflows/${workflow.id}/restore`, 'POST', {
          revision,
          base: workflow.head,
          nodeId,
        }),
      );
    } catch (error) {
      onError(error);
    }
  };
  if (authenticated === null)
    return (
      <div className="loading-page">
        <LoaderCircle className="spin" />
        Opening workspace…
        {error && <button onClick={() => void refresh()}>Retry</button>}
      </div>
    );
  if (!authenticated)
    return (
      <Login
        onLogin={() => {
          setError('');
          void refresh();
        }}
      />
    );
  return (
    <div className="app-shell">
      <nav className="rail">
        <button
          className="brand-mark"
          aria-label="Forma home"
          onClick={() => void back()}
        >
          <Layers3 size={24} />
        </button>
        <div className="rail-links">
          <button
            className={page === 'workflows' ? 'active' : ''}
            title="Workflows"
            aria-label="Workflows"
            onClick={() => {
              void back();
              setPage('workflows');
            }}
          >
            <WorkflowIcon size={20} />
          </button>
          <button
            className={page === 'components' ? 'active' : ''}
            title="Components"
            aria-label="Components"
            onClick={() => {
              void back();
              setPage('components');
            }}
          >
            <Box size={20} />
          </button>
        </div>
        <div className="rail-bottom">
          <a
            href="https://reactflow.dev/"
            target="_blank"
            rel="noreferrer"
            title="React Flow documentation"
          >
            <Code2 size={18} />
          </a>
          <button
            title="Sign out"
            aria-label="Sign out"
            onClick={() => {
              void api('/auth/logout', 'POST', {})
                .then(() => setAuthenticated(false))
                .catch(onError);
            }}
          >
            <LogOut size={18} />
          </button>
          <span className="avatar">WS</span>
        </div>
      </nav>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            <span>Workspace</span>
            <ChevronRight size={13} />
            <span>
              {workflow
                ? 'Workflows'
                : page === 'workflows'
                  ? 'Workflows'
                  : 'Components'}
            </span>
            {workflow && (
              <>
                <ChevronRight size={13} />
                <strong>{workflow.name}</strong>
              </>
            )}
          </div>
          <div className="topbar-right">
            <span className="connection-dot" />
            Platform connected
            <span className="divider" />
            <span>Local workspace</span>
          </div>
        </header>
        {error && (
          <div className="error-banner" role="alert">
            <span>{error}</span>
            {workflow && (
              <button onClick={() => void open(workflow.id)}>
                Reload saved revision
              </button>
            )}
            <button aria-label="Dismiss error" onClick={() => setError('')}>
              <X size={15} />
            </button>
          </div>
        )}
        {!workflow ? (
          <main className="dashboard">
            <div className="dashboard-heading">
              <div>
                <span className="eyebrow">YOUR WORKSPACE</span>
                <h1>
                  {page === 'workflows' ? 'Workflows' : 'Components'}
                  <span className="count">
                    {page === 'workflows' ? workflows.length : manifests.length}
                  </span>
                </h1>
                <p>
                  {page === 'workflows'
                    ? 'Connect your services. Keep every change.'
                    : 'Independent plugins. One consistent contract.'}
                </p>
              </div>
              {page === 'workflows' && (
                <button
                  className="button primary"
                  onClick={() => {
                    setName('');
                    setModal('create');
                  }}
                >
                  <Plus size={17} />
                  Create workflow
                </button>
              )}
            </div>
            {page === 'workflows' ? (
              <>
                <div className="dashboard-toolbar">
                  <div className="tab-label">
                    All workflows <span>{workflows.length}</span>
                  </div>
                  <label className="search">
                    <Search size={15} />
                    <input
                      placeholder="Find a workflow…"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                    <kbd>⌕</kbd>
                  </label>
                </div>
                <div className="workflow-table">
                  <div className="table-head">
                    <span>NAME</span>
                    <span>STATUS</span>
                    <span>VERSION</span>
                    <span>LAST UPDATED</span>
                    <span />
                  </div>
                  {workflows
                    .filter((w) =>
                      w.name.toLowerCase().includes(query.toLowerCase()),
                    )
                    .map((w) => (
                      <button
                        className="workflow-row"
                        key={w.id}
                        onClick={() => void open(w.id)}
                      >
                        <span className="workflow-name">
                          <span className="workflow-list-icon">
                            <WorkflowIcon size={20} />
                          </span>
                          <span>
                            <strong>{w.name}</strong>
                            <small>{w.id.slice(0, 8)}</small>
                          </span>
                        </span>
                        <span
                          className={`status ${w.published ? 'published' : ''}`}
                        >
                          <Circle size={6} fill="currentColor" />
                          {w.published ? 'Published' : 'Draft'}
                        </span>
                        <span className="revision-cell">
                          <GitBranch size={13} />
                          Revision {w.head}
                        </span>
                        <span className="muted">{relative(w.updatedAt)}</span>
                        <ChevronRight size={17} />
                      </button>
                    ))}
                </div>
                {workflows.length === 0 && (
                  <div className="empty-state">
                    <div className="empty-graphic">
                      <span>
                        <Box size={23} />
                      </span>
                      <i />
                      <span>
                        <WorkflowIcon size={25} />
                      </span>
                      <i />
                      <span>
                        <Radio size={23} />
                      </span>
                    </div>
                    <h2>Start with a connection.</h2>
                    <p>
                      Create a workflow, add your components,
                      <br />
                      and connect their inputs and outputs.
                    </p>
                    <button
                      className="button"
                      onClick={() => {
                        setName('');
                        setModal('create');
                      }}
                    >
                      <Plus size={16} />
                      Create your first workflow
                    </button>
                  </div>
                )}
                <div className="dashboard-footnote">
                  <ShieldCheck size={15} />
                  <span>
                    Changes are versioned. Published workflows stay pinned until
                    you publish again.
                  </span>
                  <span className="footnote-link">
                    PostgreSQL-backed history
                  </span>
                </div>
              </>
            ) : (
              <>
                <div className="dashboard-toolbar">
                  <span className="tab-label">Installed plugins</span>
                  <label className="search">
                    <Search size={15} />
                    <input
                      placeholder="Find a component…"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                  </label>
                </div>
                <div className="component-directory">
                  {filtered.map((m) => (
                    <div className="directory-row" key={m.name + m.version}>
                      <span className="catalog-icon">
                        <Box size={20} />
                      </span>
                      <div>
                        <h3>
                          {m.title}
                          <code>v{m.version}</code>
                        </h3>
                        <p>{m.description}</p>
                        <small>{m.name}</small>
                      </div>
                      <span>{m.category}</span>
                      <span className="resource-badge">{m.kind}</span>
                    </div>
                  ))}
                </div>
                <div className="sdk-callout">
                  <Code2 size={24} />
                  <div>
                    <h3>Built to be extended</h3>
                    <p>
                      Create a package with the Python SDK, declare its ports,
                      and install it through the plugin registry.
                    </p>
                    <code>workflow-plugin new your-team.your-plugin</code>
                  </div>
                </div>
              </>
            )}
          </main>
        ) : (
          <>
            <div className="editor-toolbar">
              <button
                className="icon-button"
                aria-label="Back to workflows"
                onClick={() => void back()}
              >
                <ArrowLeft size={18} />
              </button>
              <div className="editor-name">
                <strong>{workflow.name}</strong>
                <span className="status">
                  {workflow.published === workflow.head ? 'Published' : 'Draft'}
                </span>
              </div>
              <span className="save-status">
                {saveState === 'Saving…' ? (
                  <LoaderCircle size={13} className="spin" />
                ) : saveState === 'Saved' ? (
                  <Check size={13} />
                ) : (
                  <Circle size={8} />
                )}{' '}
                {saveState}
              </span>
              <div className="toolbar-actions">
                <button
                  className={`button quiet ${panel === 'history' ? 'chosen' : ''}`}
                  onClick={() =>
                    setPanel(panel === 'history' ? null : 'history')
                  }
                >
                  <History size={15} />
                  History<span className="badge">{workflow.head}</span>
                </button>
                <button
                  className="button quiet"
                  onClick={() => setPanel(panel === 'runs' ? null : 'runs')}
                >
                  <Clock3 size={15} />
                  Runs
                </button>
                <button
                  className="button"
                  disabled={!canAct}
                  onClick={() => {
                    setRunInput('{}');
                    setModal('run');
                  }}
                >
                  <Play size={14} />
                  Run
                </button>
                <button
                  className="button primary"
                  disabled={!canAct || nodes.length === 0}
                  onClick={async () => {
                    try {
                      await api(
                        `/api/workflows/${workflow.id}/publish`,
                        'POST',
                        { base: workflow.head },
                      );
                      const item = { ...workflow, published: workflow.head };
                      setWorkflow(item);
                      workflowRef.current = item;
                    } catch (error) {
                      onError(error);
                    }
                  }}
                >
                  Publish
                  <ArrowUpRight size={15} />
                </button>
              </div>
            </div>
            <div className="editor-workspace">
              {catalogOpen && (
                <aside className="catalog">
                  <div className="panel-heading">
                    <span>
                      Components{' '}
                      <span className="subtle-count">{manifests.length}</span>
                    </span>
                    <button
                      className="icon-button"
                      aria-label="Hide components"
                      onClick={() => setCatalogOpen(false)}
                    >
                      <X size={15} />
                    </button>
                  </div>
                  <label className="search">
                    <Search size={14} />
                    <input
                      placeholder="Search components…"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                  </label>
                  <select
                    className="category-select"
                    value={category}
                    onChange={(e) => setCategory(e.target.value)}
                  >
                    {[
                      'All components',
                      ...new Set(manifests.map((m) => m.category)),
                    ].map((c) => (
                      <option key={c}>{c}</option>
                    ))}
                  </select>
                  <div className="catalog-list">
                    {filtered.map((m) => (
                      <button
                        className="catalog-item"
                        key={m.name + m.version}
                        onClick={() => add(m)}
                      >
                        <span className="catalog-icon">
                          <Box size={17} />
                        </span>
                        <span>
                          <strong>{m.title}</strong>
                          <small>{m.category}</small>
                        </span>
                        <Plus size={14} />
                      </button>
                    ))}
                  </div>
                  <div className="catalog-footer">
                    <Code2 size={16} />
                    <span>
                      Your code. Your components.
                      <small>Plugin SDK · protocol v1</small>
                    </span>
                  </div>
                </aside>
              )}
              <div className="canvas">
                <select
                  className="canvas-node-picker"
                  aria-label="Select node"
                  value={selected ?? ''}
                  onChange={(e) => focusNode(e.target.value || null)}
                >
                  <option value="">Select a node to configure…</option>
                  {nodes.map((n) => (
                    <option key={n.id} value={n.id}>
                      {n.data.definition.name}
                    </option>
                  ))}
                </select>
                {!catalogOpen && (
                  <button
                    className="button show-catalog"
                    onClick={() => setCatalogOpen(true)}
                  >
                    <Plus size={15} />
                    Components
                  </button>
                )}
                <ReactFlow
                  onInit={(instance) => {
                    flowRef.current = instance;
                  }}
                  nodes={nodes}
                  edges={edges.map((edge): Edge => {
                    const output = nodes.find((node) => node.id === edge.source)?.data.manifest.outputs.find((port) => port.name === edge.sourceHandle);
                    const resource = output?.kind === 'resource';
                    return { ...edge, type: 'smoothstep', markerEnd: { type: MarkerType.ArrowClosed, color: resource ? '#475569' : '#2563eb', width: 20, height: 20 }, style: { stroke: resource ? '#475569' : '#2563eb', strokeWidth: 2, strokeDasharray: resource ? '6 4' : undefined } };
                  })}
                  nodeTypes={nodeTypes}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onConnect={connect}
                  onNodeClick={(_, n) => setSelected(n.id)}
                  onPaneClick={() => setSelected(null)}
                  fitView
                  fitViewOptions={{ maxZoom: 1, padding: 0.2 }}
                  minZoom={0.25}
                  maxZoom={1.5}
                  defaultViewport={{ x: 40, y: 40, zoom: 0.85 }}
                  deleteKeyCode={['Backspace', 'Delete']}
                  isValidConnection={(c) => {
                    if (c.source === c.target) return false;
                    const source = nodes
                      .find((n) => n.id === c.source)
                      ?.data.manifest.outputs.find(
                        (p) => p.name === c.sourceHandle,
                      );
                    const target = nodes
                      .find((n) => n.id === c.target)
                      ?.data.manifest.inputs.find(
                        (p) => p.name === c.targetHandle,
                      );
                    return (
                      !!source &&
                      !!target &&
                      source.kind === target.kind &&
                      source.resourceType === target.resourceType &&
                      (!source.sensitive || !!target.sensitive) &&
                      (!!target.multiple ||
                        !edges.some(
                          (e) =>
                            e.target === c.target &&
                            e.targetHandle === c.targetHandle,
                        ))
                    );
                  }}
                >
                  <Background
                    variant={BackgroundVariant.Dots}
                    gap={20}
                    size={1}
                    color="#d5d9d4"
                  />
                  <Controls showInteractive={false} />
                  <MiniMap
                    nodeColor="#d5dfd1"
                    maskColor="rgba(248,249,246,.8)"
                    pannable
                    zoomable
                  />
                </ReactFlow>
                {nodes.length === 0 && (
                  <div className="canvas-empty">
                    <WorkflowIcon size={32} />
                    <h2>Make the first connection.</h2>
                    <p>
                      Choose a component from the library to start building.
                    </p>
                  </div>
                )}
                <div className="canvas-caption">
                  <span className="connection-dot" />
                  {nodes.length} nodes<span>·</span>
                  {edges.length} connections<span>·</span>
                  <LockIcon />
                  Revision {workflow.head}
                </div>
              </div>
              {selectedNode && (
                <Inspector
                  key={selectedNode.id}
                  node={selectedNode.data.definition}
                  manifest={selectedNode.data.manifest}
                  onClose={() => setSelected(null)}
                  onChange={(definition) =>
                    changeGraph(
                      graphRef.current.nodes.map((n) =>
                        n.id === definition.id
                          ? { ...n, data: { ...n.data, definition } }
                          : n,
                      ),
                      graphRef.current.edges,
                    )
                  }
                  onDelete={() => {
                    changeGraph(
                      nodes.filter((n) => n.id !== selected),
                      edges.filter(
                        (e) => e.source !== selected && e.target !== selected,
                      ),
                    );
                    setSelected(null);
                  }}
                  onDuplicate={() => {
                    const existing = graphRef.current.nodes.find(n => n.id === selectedNode.id);
                    if (!existing) return;
                    const newId = crypto.randomUUID();
                    const position = { x: existing.position.x + 30, y: existing.position.y + 30 };
                    changeGraph([
                      ...graphRef.current.nodes.map(n => n.id === existing.id ? { ...n, selected: false } : n),
                      {
                        ...existing,
                        id: newId,
                        position,
                        selected: true,
                        data: {
                          ...existing.data,
                          definition: { ...existing.data.definition, id: newId, position }
                        }
                      }
                    ], graphRef.current.edges);
                    setSelected(newId);
                  }}
                  workflowId={workflow.id}
                  saved={canAct}
                  published={workflow.published}
                  onRunReceived={(id) => {
                    setActiveRun(id);
                    setPanel('runs');
                    setSteps([]);
                  }}
                  onVersion={(version) => {
                    const m =
                      catalog[`${selectedNode.data.manifest.name}@${version}`];
                    changeGraph(
                      graphRef.current.nodes.map((n) =>
                        n.id === selectedNode.id
                          ? {
                              ...n,
                              data: {
                                manifest: m,
                                definition: { ...n.data.definition, version },
                              },
                            }
                          : n,
                      ),
                      graphRef.current.edges,
                    );
                  }}
                  bindings={{
                    nodes,
                    edges,
                    catalog,
                    onBind: (input, source, output) =>
                      connect({
                        source,
                        sourceHandle: output,
                        target: selectedNode.id,
                        targetHandle: input,
                      }),
                    onUnbind: (id) =>
                      changeGraph(
                        graphRef.current.nodes,
                        graphRef.current.edges.filter((e) => e.id !== id),
                      ),
                    onAddSource: (input, m, output) =>
                      add(m, { nodeId: selectedNode.id, input, output }),
                    onSelect: focusNode,
                  }}
                />
              )}
            </div>
            {panel && (
              <div className="bottom-panel">
                <div className="panel-heading">
                  <span>
                    {panel === 'history' ? (
                      <>
                        <History size={15} />
                        Revision history
                      </>
                    ) : (
                      <>
                        <Radio size={15} />
                        Workflow runs
                      </>
                    )}
                  </span>
                  <button
                    className="icon-button"
                    aria-label="Close panel"
                    onClick={() => setPanel(null)}
                  >
                    <X size={16} />
                  </button>
                </div>
                {panel === 'history' ? (
                  <div className="history-list">
                    {history.map((h) => (
                      <div className="history-row" key={h.revision}>
                        <span className="revision-marker">{h.revision}</span>
                        <div>
                          <strong>{h.message || 'Workflow created'}</strong>
                          <small>
                            {h.author} · {relative(h.createdAt)} ·{' '}
                            {h.graph.nodes.length} nodes
                          </small>
                        </div>
                        {h.revision === workflow.head ? (
                          <span className="status published">Current</span>
                        ) : (
                          <>
                            <button
                              className="text-button"
                              disabled={!canAct}
                              onClick={() => void restore(h.revision)}
                            >
                              Restore workflow
                            </button>
                            {selected && (
                              <button
                                className="text-button"
                                disabled={!canAct}
                                onClick={() =>
                                  void restore(h.revision, selected)
                                }
                              >
                                Restore selected node
                              </button>
                            )}
                          </>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="runs-layout">
                    <div className="runs-list">
                      {runs.length === 0 ? (
                        <p className="panel-empty">
                          No runs yet. Run this workflow to inspect its
                          execution.
                        </p>
                      ) : (
                        runs.map((run) => (
                          <div
                            className={`run-row ${activeRun === run.id ? 'active' : ''}`}
                            key={run.id}
                          >
                            <button
                              onClick={() => {
                                setActiveRun(run.id);
                                setSteps([]);
                              }}
                            >
                              <span className={`status ${run.status}`}>
                                {run.status}
                              </span>
                              <code>{run.id.slice(0, 8)}</code>
                              <span>
                                r{run.revision} · attempt {run.attempt}
                              </span>
                              <small>{relative(run.createdAt)}</small>
                            </button>
                            {['queued', 'running'].includes(run.status) && (
                              <button
                                className="text-button"
                                onClick={() =>
                                  void api(
                                    `/api/runs/${run.id}/cancel`,
                                    'POST',
                                    {},
                                  ).catch(onError)
                                }
                              >
                                Cancel
                              </button>
                            )}
                          </div>
                        ))
                      )}
                    </div>
                    <div className="step-list">
                      {activeRun ? (
                        steps.map((step) => (
                          <div key={step.nodeId + step.attempt}>
                            <span className={`status ${step.status}`}>
                              {step.status}
                            </span>
                            <strong>
                              {nodes.find((n) => n.id === step.nodeId)?.data
                                .definition.name ?? step.nodeId}
                            </strong>
                            <small>
                              {step.error ?? `Attempt ${step.attempt}`}
                            </small>
                          </div>
                        ))
                      ) : (
                        <p className="panel-empty">
                          Select a run to inspect its steps.
                        </p>
                      )}
                    </div>
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>
      {modal && (
        <div className="modal-scrim">
          <section
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label={
              modal === 'create'
                ? 'Create workflow'
                : modal === 'run'
                  ? 'Run workflow'
                  : 'Webhook binding'
            }
          >
            <button
              className="icon-button modal-close"
              aria-label="Close dialog"
              onClick={() => setModal(null)}
            >
              <X size={19} />
            </button>
            {modal === 'create' ? (
              <form
                onSubmit={async (e) => {
                  e.preventDefault();
                  try {
                    const item = await api<Workflow>('/api/workflows', 'POST', {
                      name,
                      graph: emptyGraph,
                      base: 0,
                      message: 'Created workflow',
                    });
                    install(item);
                    setModal(null);
                    setPage('workflows');
                  } catch (error) {
                    onError(error);
                  }
                }}
              >
                <span className="eyebrow">NEW WORKFLOW</span>
                <h2>Give your workflow a name.</h2>
                <p>You can start connecting components right away.</p>
                <label className="field">
                  Workflow name
                  <input
                    autoFocus
                    required
                    maxLength={160}
                    placeholder="Customer support agent"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </label>
                <button className="button primary full" type="submit">
                  Create workflow
                  <ArrowUpRight size={16} />
                </button>
              </form>
            ) : modal === 'run' ? (
              <form
                onSubmit={async (e) => {
                  e.preventDefault();
                  try {
                    const input: unknown = JSON.parse(runInput);
                    const result = await api<{ id: string }>(
                      `/api/workflows/${workflow?.id}/runs`,
                      'POST',
                      { input },
                    );
                    setActiveRun(result.id);
                    setSteps([]);
                    setPanel('runs');
                    setModal(null);
                  } catch (error) {
                    onError(error);
                  }
                }}
              >
                <span className="eyebrow">RUN REVISION {workflow?.head}</span>
                <h2>Run this workflow.</h2>
                <p>Provide a JSON object for the trigger input.</p>
                <label className="field">
                  Input
                  <textarea
                    className="code-input"
                    rows={9}
                    value={runInput}
                    onChange={(e) => setRunInput(e.target.value)}
                  />
                </label>
                <button className="button primary full">
                  <Play size={15} />
                  Start run
                </button>
              </form>
            ) : null}
          </section>
        </div>
      )}
    </div>
  );
}
function LockIcon() {
  return <ShieldCheck size={12} />;
}
