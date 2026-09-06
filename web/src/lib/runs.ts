import { useEffect, useState } from 'react';
import { api } from './api';
import type { Revision, Run, RunDetail, Step } from './types';

export const POLL_INTERVAL = 2500;

/**
 * The history and run panels for one workflow.
 *
 * Polls only while a panel is open, and discards any response that arrives after
 * the caller moved on, so a slow request cannot repopulate a closed panel or a
 * workflow the editor has already left.
 */
export function useRunFeed(
  workflowId: string | null,
  panel: 'history' | 'runs' | null,
  activeRun: string | null,
  onError: (error: unknown) => void,
) {
  const [history, setHistory] = useState<Revision[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [steps, setSteps] = useState<Step[]>([]);
  const [detail, setDetail] = useState<RunDetail | null>(null);

  useEffect(() => {
    if (!workflowId || !panel) return;
    let stale = false;
    const load = async () => {
      try {
        if (panel === 'history') {
          const result = await api<Revision[]>(`/api/workflows/${workflowId}/history`);
          if (!stale) setHistory(result);
          return;
        }
        const result = await api<Run[]>(`/api/workflows/${workflowId}/runs`);
        if (!stale) setRuns(result);
        if (!activeRun) return;
        const [stepResult, detailResult] = await Promise.all([
          api<Step[]>(`/api/runs/${activeRun}/steps`),
          api<RunDetail>(`/api/runs/${activeRun}`),
        ]);
        if (!stale) {
          setSteps(stepResult);
          setDetail(detailResult);
        }
      } catch (error) {
        if (!stale) onError(error);
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), POLL_INTERVAL);
    return () => {
      stale = true;
      window.clearInterval(timer);
    };
  }, [workflowId, panel, activeRun, onError]);

  return { history, runs, steps, detail, setSteps, setDetail };
}
