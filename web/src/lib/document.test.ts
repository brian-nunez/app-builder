import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AUTOSAVE_INTERVAL, useWorkflowDocument } from './document';
import type { Manifest, Workflow } from './types';

const manifest = (name: string, version: string): Manifest => ({
  protocol: 'workflow.plugin/v1',
  name,
  version,
  title: name,
  description: '',
  category: 'Test',
  kind: 'action',
  configSchema: { type: 'object', properties: { retries: { type: 'integer', default: 3 } } },
  inputs: [],
  outputs: [],
  permissions: [],
  digest: '',
});

const catalog = { 'team.example@2.0.0': manifest('team.example', '2.0.0') };

const workflow = (overrides: Partial<Workflow> = {}): Workflow => ({
  id: 'w1',
  name: 'Nightly sync',
  head: 4,
  published: null,
  updatedAt: '2026-09-05T00:00:00Z',
  graph: { nodes: [], edges: [] },
  ...overrides,
});

const node = (version: string) => ({
  id: 'n1',
  plugin: 'team.example',
  version,
  name: 'Example',
  config: { retries: 5 },
  position: { x: 0, y: 0 },
});

const upToDate = workflow({ graph: { nodes: [node('2.0.0')], edges: [] } });

const pinnedToOldVersion = workflow({
  graph: { nodes: [node('1.0.0')], edges: [] },
});

let fetchMock: ReturnType<typeof vi.fn>;

function respond(body: unknown, ok = true, status = 200) {
  return Promise.resolve({
    ok,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

beforeEach(() => {
  vi.useFakeTimers();
  fetchMock = vi.fn(() => respond(workflow({ head: 5 })));
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

/** Advance past one autosave tick and let the save it starts settle. */
async function tick() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(AUTOSAVE_INTERVAL);
  });
}

describe('useWorkflowDocument', () => {
  it('opens a workflow as saved', () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(workflow()));
    expect(result.current.status).toBe('saved');
    expect(result.current.label).toBe('Saved');
    expect(result.current.settled()).toBe(true);
  });

  it('upgrades a node pinned to an uninstalled version and keeps its configuration', () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(pinnedToOldVersion));

    const upgraded = result.current.nodes[0].data.definition;
    expect(upgraded.version).toBe('2.0.0');
    expect(upgraded.config.retries).toBe(5);
    expect(result.current.status).toBe('upgrading');
    expect(result.current.notice).toContain('upgraded');
    expect(result.current.settled()).toBe(false);
  });

  it('marks a node whose plugin is gone rather than dropping it', () => {
    const { result } = renderHook(() => useWorkflowDocument({}, () => {}));
    act(() => result.current.open(pinnedToOldVersion));
    expect(result.current.nodes).toHaveLength(1);
    expect(result.current.nodes[0].data.manifest.title).toBe('Unavailable plugin');
  });

  it('autosaves an edit and returns to saved', async () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(workflow()));
    act(() => result.current.apply([], []));
    expect(result.current.status).toBe('unsaved');

    await tick();

    expect(result.current.status).toBe('saved');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe('/api/workflows/w1');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body).base).toBe(4);
    // The response advanced head, so the next save builds on it.
    expect(result.current.workflow?.head).toBe(5);
  });

  it('stops retrying after a rejected save until the next edit', async () => {
    const errors: unknown[] = [];
    fetchMock.mockImplementation(() => respond({ error: 'workflow changed' }, false, 409));
    const { result } = renderHook(() =>
      useWorkflowDocument(catalog, (error) => errors.push(error)),
    );
    act(() => result.current.open(workflow()));
    act(() => result.current.apply([], []));

    await tick();
    expect(result.current.status).toBe('failed');
    expect(errors).toHaveLength(1);

    // A conflict must not be hammered every interval.
    await tick();
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(1);

    // The next edit clears the block and tries again.
    fetchMock.mockImplementation(() => respond(workflow({ head: 5 })));
    act(() => result.current.apply([], []));
    await tick();
    expect(result.current.status).toBe('saved');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('discards a save response for a workflow the editor has left', async () => {
    let release: (value: Response) => void = () => {};
    fetchMock.mockImplementation(
      () => new Promise<Response>((resolve) => (release = resolve)),
    );
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(workflow()));
    act(() => result.current.apply([], []));
    await tick();

    // Open a different workflow while the first save is still in flight.
    act(() => result.current.open(workflow({ id: 'w2', name: 'Other' })));
    await act(async () => {
      release({
        ok: true,
        status: 200,
        json: () => Promise.resolve(workflow({ id: 'w1', name: 'Nightly sync', head: 9 })),
        text: () => Promise.resolve(''),
      } as Response);
      await Promise.resolve();
    });

    expect(result.current.workflow?.id).toBe('w2');
    expect(result.current.workflow?.name).toBe('Other');
  });

  it('does not save when only the selection changed', async () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(upToDate));
    act(() =>
      result.current.onNodesChange([{ id: 'n1', type: 'select', selected: true }]),
    );
    expect(result.current.status).toBe('saved');
    await tick();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('renaming the open workflow marks it unsaved', () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(workflow()));
    act(() => result.current.rename('Renamed'));
    expect(result.current.workflow?.name).toBe('Renamed');
    expect(result.current.status).toBe('unsaved');
  });

  it('closing clears the document', () => {
    const { result } = renderHook(() => useWorkflowDocument(catalog, () => {}));
    act(() => result.current.open(pinnedToOldVersion));
    act(() => result.current.close());
    expect(result.current.workflow).toBeNull();
    expect(result.current.nodes).toEqual([]);
    expect(result.current.settled()).toBe(true);
  });
});
