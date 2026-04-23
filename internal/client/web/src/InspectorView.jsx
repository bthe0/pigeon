import { useState, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { Pill } from './Shared';
import { statusColor } from './Constants';
import styles from './InspectorView.module.css';

const METHOD_COLORS = { GET:'#4d9fff', POST:'#00e87a', PUT:'#f5c542', DELETE:'#ff4d4d', PATCH:'#c084fc', OPTIONS:'#6b7068', HEAD:'#9ba39c' };

export function InspectorView({ tunnels, dashFetch }) {
  const [selected, setSelected] = useState(null);
  const [requests, setRequests] = useState([]);
  const [filterTunnel, setFilterTunnel] = useState('all');
  const [liveMode, setLiveMode] = useState(true);
  const [loading, setLoading] = useState(true);
  const [replayResult, setReplayResult] = useState(null);
  const [replaying, setReplaying] = useState(false);
  const [editMode, setEditMode] = useState(false);
  const [editHeaders, setEditHeaders] = useState([]);
  const [editBody, setEditBody] = useState('');

  useEffect(() => {
    setEditMode(false);
    setReplayResult(null);
  }, [selected?.id]);

  function startEdit() {
    if (!selected) return;
    const entries = Object.entries(selected.request_headers || {}).map(([k, v]) => [k, v]);
    setEditHeaders(entries);
    setEditBody(selected.request_body || '');
    setReplayResult(null);
    setEditMode(true);
  }

  function cancelEdit() {
    setEditMode(false);
  }

  async function sendReplay() {
    if (!selected) return;
    setReplaying(true);
    setReplayResult(null);
    try {
      const headersObj = {};
      for (const [k, v] of editHeaders) {
        const key = (k || '').trim();
        if (key) headersObj[key] = v;
      }
      const res = await dashFetch('/api/inspector/replay', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          forward_id: selected.forward_id,
          domain: selected.domain,
          method: selected.method,
          path: selected.path,
          headers: headersObj,
          body: editBody,
          body_encoding: selected.request_body_encoding || ''
        })
      });
      const text = await res.text();
      let parsed = null;
      try { parsed = JSON.parse(text); } catch (_) {}
      if (!res.ok) {
        setReplayResult({ error: parsed?.error || text || 'Replay failed' });
      } else {
        setReplayResult(parsed);
        setEditMode(false);
      }
    } catch (err) {
      setReplayResult({ error: err.message });
    } finally {
      setReplaying(false);
    }
  }

  function updateHeaderKey(idx, key) {
    setEditHeaders(hs => hs.map((h, i) => i === idx ? [key, h[1]] : h));
  }
  function updateHeaderValue(idx, value) {
    setEditHeaders(hs => hs.map((h, i) => i === idx ? [h[0], value] : h));
  }
  function removeHeader(idx) {
    setEditHeaders(hs => hs.filter((_, i) => i !== idx));
  }
  function addHeader() {
    setEditHeaders(hs => [...hs, ['', '']]);
  }

  useEffect(() => {
    if (!liveMode) return;
    const fetchInspector = async () => {
      try {
        const qs = filterTunnel === 'all' ? '' : `?filter=${encodeURIComponent(filterTunnel)}`;
        const res = await dashFetch(`/api/inspector${qs}`);
        if (!res.ok) throw new Error(await res.text());
        const raw = await res.json() || [];
        const rows = [...raw].reverse().map(r => {
          const d = new Date(r.time);
          const time = `${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}:${String(d.getSeconds()).padStart(2,'0')}`;
          return {
            ...r,
            id: `${r.time}-${r.forward_id}-${r.method}-${r.path}`,
            time,
            tunnel: r.domain || r.forward_id,
            ms: r.duration_ms || 0,
            size: r.bytes ? `${r.bytes} B` : '—',
            ip: r.remote_addr || '—'
          };
        });
        setRequests(rows);
        setLoading(false);
        if (selected) {
          const next = rows.find(r => r.id === selected.id);
          if (next) setSelected(next);
        }
      } catch (err) {
        setLoading(false);
      }
    };
    fetchInspector();
    const interval = setInterval(fetchInspector, 1000);
    return () => clearInterval(interval);
  }, [liveMode, filterTunnel]);

  const onlineTunnels = tunnels.filter(t => t.status === 'online');

  return (
    <div className={styles.layout}>
      <div className={styles.list}>
        <div className={styles.listHeader}>
          <div className={styles.titleWrap}>
            <div className={styles.title}>Request Inspector</div>
            <div className={styles.subtitle}>{requests.length} requests captured</div>
          </div>
          <button
            onClick={() => setLiveMode(x => !x)}
            className={`${styles.liveBtn} ${liveMode ? styles.liveBtnActive : ''}`}
          >
            <span className={`${styles.liveDot} ${liveMode ? styles.liveDotActive : ''}`} />
            {liveMode ? 'LIVE' : 'PAUSED'}
          </button>
          <select
            value={filterTunnel}
            onChange={e => setFilterTunnel(e.target.value)}
            className={styles.filterSelect}
          >
            <option value="all">All tunnels</option>
            {onlineTunnels.map(t => <option key={t.id} value={t.publicUrl || t.id}>{t.publicUrl || t.id}</option>)}
          </select>
        </div>

        <div className={styles.columns}>
          {['Time','Method','Path','Status','Dur.'].map(h => (
            <div key={h} className={styles.columnLabel}>{h}</div>
          ))}
        </div>

        <div className={styles.rows}>
          {loading ? (
            <div className={styles.empty}>Loading inspector…</div>
          ) : requests.length === 0 ? (
            <div className={styles.empty}>No HTTP requests captured yet.</div>
          ) : requests.map(r => (
            <div
              key={r.id}
              onClick={() => setSelected(r)}
              className={`${styles.row} ${selected?.id === r.id ? styles.rowSelected : ''}`}
            >
              <span className={styles.rowTime}>{r.time}</span>
              <span className={styles.rowMethod} style={{ color: METHOD_COLORS[r.method] || '#9ba39c' }}>{r.method}</span>
              <span className={styles.rowPath}>{r.path}</span>
              <span className={styles.rowStatus} style={{ color: statusColor(r.status) }}>{r.status}</span>
              <span className={styles.rowMs}>{r.ms}ms</span>
            </div>
          ))}
        </div>
      </div>

      <div className={styles.detail}>
        {selected ? (
          <>
            <div className={styles.detailHeader}>
              <div className={styles.detailTopRow}>
                <Pill color={METHOD_COLORS[selected.method] || '#9ba39c'}>{selected.method}</Pill>
                <span className={styles.detailPath}>{selected.path}</span>
                <span className={styles.detailStatus} style={{ color: statusColor(selected.status) }}>{selected.status}</span>
                {editMode ? (
                  <>
                    <button onClick={cancelEdit} disabled={replaying} className={styles.cancelBtn}>Cancel</button>
                    <button onClick={sendReplay} disabled={replaying} className={styles.sendBtn}>
                      {replaying ? 'Sending…' : 'Send'}
                    </button>
                  </>
                ) : (
                  <button onClick={startEdit} disabled={replaying} className={styles.replayBtn}>Replay</button>
                )}
              </div>
              <div className={styles.metaRow}>
                {[['Tunnel', selected.tunnel], ['IP', selected.ip], ['Duration', selected.ms + 'ms'], ['Size', selected.size], ['Time', selected.time]].map(([k, v]) => (
                  <div key={k}>
                    <span className={styles.metaKey}>{k} </span>
                    <span className={styles.metaValue}>{v}</span>
                  </div>
                ))}
              </div>
            </div>

            <div className={styles.body}>
              <div className={styles.section}>
                <div className={styles.sectionHeader}>
                  <div className={styles.sectionLabel}>Request Headers</div>
                  {editMode && <Pill color="var(--accent)">editing</Pill>}
                </div>
                {editMode ? (
                  <div className={styles.editorBox}>
                    {editHeaders.length === 0 && <div className={styles.editorNote}>No headers.</div>}
                    {editHeaders.map(([k, v], idx) => (
                      <div key={idx} className={styles.editorRow}>
                        <input value={k} onChange={e => updateHeaderKey(idx, e.target.value)} placeholder="Header" disabled={replaying} className={styles.editorInputKey} />
                        <input value={v} onChange={e => updateHeaderValue(idx, e.target.value)} placeholder="Value" disabled={replaying} className={styles.editorInputValue} />
                        <button type="button" onClick={() => removeHeader(idx)} disabled={replaying} className={styles.trashBtn}>
                          <Icon d={Icons.trash} size={11} color="#ff4d4d" />
                        </button>
                      </div>
                    ))}
                    <button type="button" onClick={addHeader} disabled={replaying} className={styles.addHeaderBtn}>+ Add header</button>
                  </div>
                ) : (
                  <div className={styles.headersBox}>
                    {Object.keys(selected.request_headers || {}).length > 0 ? Object.entries(selected.request_headers).map(([k, v]) => (
                      <div key={k} className={styles.headerLine}>
                        <span className={styles.headerKey}>{k}: </span>
                        <span className={styles.headerValue}>{v}</span>
                      </div>
                    )) : (
                      <span className={styles.emptyText}>No headers recorded.</span>
                    )}
                  </div>
                )}
              </div>

              <div className={styles.section}>
                <div className={`${styles.sectionLabel} ${styles.sectionLabelSpaced}`}>Response Headers</div>
                <div className={styles.headersBox}>
                  {Object.keys(selected.response_headers || {}).length > 0 ? Object.entries(selected.response_headers).map(([k, v]) => (
                    <div key={k} className={styles.headerLine}>
                      <span className={styles.headerKey}>{k}: </span>
                      <span className={styles.headerValue}>{v}</span>
                    </div>
                  )) : (
                    <span className={styles.emptyText}>No headers recorded.</span>
                  )}
                </div>
              </div>

              <div className={styles.section}>
                <div className={styles.sectionHeader}>
                  <div className={styles.sectionLabel}>Request Body</div>
                  {selected.request_body_encoding === 'base64' && <Pill color="#f5c542">base64</Pill>}
                  {selected.request_body_truncated && <Pill color="#ff4d4d">truncated</Pill>}
                  {editMode && <Pill color="var(--accent)">editing</Pill>}
                </div>
                {editMode ? (
                  <textarea value={editBody} onChange={e => setEditBody(e.target.value)} disabled={replaying} rows={8} className={styles.textarea} />
                ) : (
                  <pre className={`${styles.pre} ${selected.request_body ? '' : styles.preEmpty}`}>
                    {selected.request_body || 'Body capture disabled for this tunnel.'}
                  </pre>
                )}
              </div>

              <div className={styles.section}>
                <div className={styles.sectionHeader}>
                  <div className={styles.sectionLabel}>Response Body</div>
                  {selected.response_body_encoding === 'base64' && <Pill color="#f5c542">base64</Pill>}
                  {selected.response_body_truncated && <Pill color="#ff4d4d">truncated</Pill>}
                </div>
                <pre className={`${styles.pre} ${selected.response_body ? '' : styles.preEmpty}`}>
                  {selected.response_body || 'Body capture disabled for this tunnel.'}
                </pre>
              </div>

              {replayResult && (
                <div className={styles.replaySection}>
                  <div className={styles.replayLabel}>Replay result</div>
                  {replayResult.error ? (
                    <div className={styles.replayError}>{replayResult.error}</div>
                  ) : (
                    <div>
                      <div className={styles.replayMeta}>
                        <span className={styles.replayStatus} style={{ color: statusColor(replayResult.status) }}>{replayResult.status}</span>
                        <span className={styles.replayDuration}>{replayResult.duration_ms}ms</span>
                        {replayResult.truncated && <Pill color="#ff4d4d">truncated</Pill>}
                      </div>
                      <pre className={styles.pre}>{replayResult.body || '(empty response)'}</pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          </>
        ) : (
          <div className={styles.placeholder}>
            <Icon d={Icons.activity} size={32} color="var(--border2)" />
            <span>Select a request to inspect</span>
          </div>
        )}
      </div>
    </div>
  );
}
