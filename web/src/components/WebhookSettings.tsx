import { useEffect, useState } from 'react';
import { api } from '../lib/api';
type Binding = {
  id: string;
  nodeId: string;
  mode: string;
  path: string;
  token: string;
  runId?: string;
  receivedAt?: string;
};
export function WebhookSettings({
  workflowId,
  nodeId,
  saved,
  published,
  onRunReceived,
}: {
  workflowId: string;
  nodeId: string;
  saved: boolean;
  published: number | null;
  onRunReceived: (id: string) => void;
}) {
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [mode, setMode] = useState('draft');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [reply, setReply] = useState('');
  const [copied, setCopied] = useState('');
  const [body, setBody] = useState('{"message":"Hello"}');
  useEffect(() => {
    let active = true;
    const refresh = async () => {
      try {
        const result = await api<Binding[]>(
          `/api/workflows/${workflowId}/bindings`,
        );
        if (active) setBindings(result.filter((b) => b.nodeId === nodeId));
      } catch (e) {
        if (active) setError(String(e));
      }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 2500);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [workflowId, nodeId]);
  const selected = bindings.find((b) => b.mode === mode && b.token);
  const url = selected ? window.location.origin + selected.path : '';
  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(label);
    } catch {
      setError(
        'Clipboard access was denied. Select and copy the displayed value.',
      );
    }
  };
  return (
    <section className="webhook-settings">
      <h3>Receive a webhook</h3>
      <p>
        Create an endpoint, then POST JSON with its bearer token. Test endpoints
        use the saved draft; live endpoints use the published revision. Select a
        webhook response output in a node’s settings to return its result.
      </p>
      <div className="tabs" role="group" aria-label="Webhook endpoint mode">
        <button
          type="button"
          className={mode === 'draft' ? 'active' : ''}
          onClick={() => setMode('draft')}
        >
          Test endpoint
        </button>
        <button
          type="button"
          className={mode === 'published' ? 'active' : ''}
          onClick={() => setMode('published')}
        >
          Live endpoint
        </button>
      </div>
      {selected ? (
        <>
          <label className="field">
            POST URL
            <input readOnly value={url} />
          </label>
          <button
            type="button"
            className="button"
            onClick={() => void copy(url, 'URL')}
          >
            Copy URL
          </button>
          <label className="field">
            Bearer token
            <input type="password" readOnly value={selected.token} />
          </label>
              <label className="field">
                Request JSON
                <textarea
                  rows={3}
                  className="code-input"
                  value={body}
                  onChange={(e) => setBody(e.target.value)}
                />
              </label>
          <div className="webhook-actions">
            <button
              type="button"
              className="button"
              onClick={() => void copy(selected.token, 'token')}
            >
              Copy token
            </button>
            <button
              type="button"
              className="button"
              onClick={() =>
                void copy(
                  `curl --request POST '${url}' --header 'Authorization: Bearer ${selected.token}' --header 'Content-Type: application/json' --data '${body.replaceAll("'", "'\\''")}'`,
                  'curl command',
                )
              }
            >
              Copy curl command
            </button>
          </div>
          {mode === 'draft' && (
            <>
              <button
                type="button"
                className="button primary full"
                disabled={busy || !saved}
                onClick={async () => {
                  setBusy(true);
                  setError('');
                  setReply('');
                  try {
                    const response = await fetch(selected.path, {
                      method: 'POST',
                      headers: {
                        'Content-Type': 'application/json',
                        Authorization: `Bearer ${selected.token}`,
                      },
                      body: JSON.stringify(JSON.parse(body)),
                    });
                    const result = await response.json();
                    if (!response.ok)
                      throw new Error(result.error ?? 'Request rejected');
                    setReply(JSON.stringify(result, null, 2));
                    onRunReceived(result.runId);
                    setBindings((old) =>
                      old.map((b) =>
                        b.id === selected.id
                          ? {
                              ...b,
                              runId: result.runId,
                              receivedAt: new Date().toISOString(),
                            }
                          : b,
                      ),
                    );
                  } catch (e) {
                    setError(e instanceof Error ? e.message : String(e));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                {busy ? 'Sending…' : 'Send test POST'}
              </button>
            </>
          )}
          {reply && <pre className="code-input" aria-label="Webhook response" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{reply}</pre>}
          <div className="webhook-receipt" role="status">
            {selected.receivedAt ? (
              <>
                Last request:{' '}
                {new Date(selected.receivedAt).toLocaleTimeString()}
                <button
                  type="button"
                  className="text-button"
                  onClick={() =>
                    selected.runId && onRunReceived(selected.runId)
                  }
                >
                  View received run
                </button>
              </>
            ) : (
              'Waiting for a request.'
            )}
          </div>
        </>
      ) : (
        <button
          type="button"
          className="button primary full"
          disabled={!saved || busy || (mode === 'published' && !published)}
          onClick={async () => {
            setBusy(true);
            setError('');
            try {
              const result = await api<Binding>(
                `/api/workflows/${workflowId}/bindings`,
                'POST',
                { nodeId, mode },
              );
              setBindings((old) => [result, ...old]);
            } catch (e) {
              setError(e instanceof Error ? e.message : String(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          Create {mode === 'draft' ? 'test' : 'live'} endpoint
        </button>
      )}
      {!saved && <p>Waiting for the workflow to save.</p>}
      {mode === 'published' && !published && (
        <p>Publish the workflow to enable its live endpoint.</p>
      )}

      {copied && <small role="status">Copied {copied}.</small>}
      {error && (
        <p role="alert" className="field-error">
          {error}
        </p>
      )}
    </section>
  );
}
