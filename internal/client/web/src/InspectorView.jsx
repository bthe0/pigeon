import React, { useState, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { Pill } from './Shared';
import { statusColor } from './Constants';

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
    <div className="inspector-layout" style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
      <div className="inspector-list" style={{ flex: '0 0 480px', display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--border)' }}>
        <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 15, fontWeight: 600, color: '#fff' }}>Request Inspector</div>
            <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>{requests.length} requests captured</div>
          </div>
          <button onClick={() => setLiveMode(x=>!x)}
            style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 10px', background: liveMode ? 'var(--accent-dim)' : 'var(--panel2)', border: `1px solid ${liveMode ? 'var(--accent-mid)' : 'var(--border2)'}`, color: liveMode ? 'var(--accent)' : 'var(--text-dim)', fontSize: 11, fontWeight: 600, cursor: 'pointer', letterSpacing: '.04em' }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: liveMode ? 'var(--accent)' : 'var(--text-dim)', display: 'inline-block', animation: liveMode ? 'pulse 1.5s ease infinite' : 'none' }} />
            {liveMode ? 'LIVE' : 'PAUSED'}
          </button>
          <select value={filterTunnel} onChange={e=>setFilterTunnel(e.target.value)}
            style={{ background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '5px 8px', fontSize: 11, fontFamily: 'var(--mono)', outline: 'none' }}>
            <option value="all">All tunnels</option>
            {onlineTunnels.map(t => <option key={t.id} value={t.publicUrl || t.id}>{t.publicUrl || t.id}</option>)}
          </select>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '60px 60px 1fr 50px 50px', gap: '0 8px', padding: '5px 16px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
          {['Time','Method','Path','Status','Dur.'].map(h=>(
            <div key={h} style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>{h}</div>
          ))}
        </div>

        <div style={{ flex: 1, overflowY: 'auto' }}>
          {loading ? (
            <div style={{ padding: 24, color: 'var(--text-dim)', fontSize: 12 }}>Loading inspector…</div>
          ) : requests.length === 0 ? (
            <div style={{ padding: 24, color: 'var(--text-dim)', fontSize: 12 }}>No HTTP requests captured yet.</div>
          ) : requests.map(r => (
            <div key={r.id} onClick={() => setSelected(r)}
              style={{ display: 'grid', gridTemplateColumns: '60px 60px 1fr 50px 50px', gap: '0 8px', padding: '8px 16px', borderBottom: '1px solid var(--border)', cursor: 'pointer', background: selected?.id === r.id ? 'var(--accent-dim)' : 'transparent', borderLeft: selected?.id === r.id ? '2px solid var(--accent)' : '2px solid transparent', transition: 'background .1s', alignItems: 'center' }}>
              <span style={{ fontFamily: 'var(--mono)', fontSize: 10, color: 'var(--text-dim)' }}>{r.time}</span>
              <span style={{ fontFamily: 'var(--mono)', fontSize: 10, fontWeight: 600, color: METHOD_COLORS[r.method] || '#9ba39c' }}>{r.method}</span>
              <span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.path}</span>
              <span style={{ fontFamily: 'var(--mono)', fontSize: 11, fontWeight: 600, color: statusColor(r.status) }}>{r.status}</span>
              <span style={{ fontFamily: 'var(--mono)', fontSize: 10, color: 'var(--text-dim)' }}>{r.ms}ms</span>
            </div>
          ))}
        </div>
      </div>

      <div className="inspector-detail" style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {selected ? (
          <>
            <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <Pill color={METHOD_COLORS[selected.method] || '#9ba39c'}>{selected.method}</Pill>
                <span style={{ fontFamily: 'var(--mono)', fontSize: 13, color: '#fff', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{selected.path}</span>
                <span style={{ fontFamily: 'var(--mono)', fontSize: 12, fontWeight: 600, color: statusColor(selected.status) }}>{selected.status}</span>
                {editMode ? (
                  <>
                    <button onClick={cancelEdit} disabled={replaying}
                      style={{ background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '5px 12px', fontSize: 11, fontWeight: 600, cursor: replaying ? 'default' : 'pointer', letterSpacing: '.03em' }}>
                      Cancel
                    </button>
                    <button onClick={sendReplay} disabled={replaying}
                      style={{ display: 'flex', alignItems: 'center', gap: 5, background: replaying ? 'var(--accent-mid)' : 'var(--accent)', border: 'none', color: '#000', padding: '5px 12px', fontSize: 11, fontWeight: 600, cursor: replaying ? 'default' : 'pointer', letterSpacing: '.03em' }}>
                      {replaying ? 'Sending…' : 'Send'}
                    </button>
                  </>
                ) : (
                  <button onClick={startEdit} disabled={replaying}
                    style={{ display: 'flex', alignItems: 'center', gap: 5, background: 'var(--accent)', border: 'none', color: '#000', padding: '5px 12px', fontSize: 11, fontWeight: 600, cursor: 'pointer', letterSpacing: '.03em' }}>
                    Replay
                  </button>
                )}
              </div>
              <div style={{ marginTop: 6, display: 'flex', gap: 16, flexWrap: 'wrap' }}>
                {[['Tunnel', selected.tunnel], ['IP', selected.ip], ['Duration', selected.ms+'ms'], ['Size', selected.size], ['Time', selected.time]].map(([k,v]) => (
                  <div key={k}>
                    <span style={{ fontSize: 10, color: 'var(--text-dim)', textTransform: 'uppercase', letterSpacing: '.06em' }}>{k} </span>
                    <span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-mid)' }}>{v}</span>
                  </div>
                ))}
              </div>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', padding: 24 }}>
              <div style={{ marginBottom: 20 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>Request Headers</div>
                  {editMode && <Pill color="var(--accent)">editing</Pill>}
                </div>
                {editMode ? (
                  <div style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '10px 12px' }}>
                    {editHeaders.length === 0 && (
                      <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)', marginBottom: 6 }}>No headers.</div>
                    )}
                    {editHeaders.map(([k, v], idx) => (
                      <div key={idx} style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
                        <input value={k} onChange={e => updateHeaderKey(idx, e.target.value)} placeholder="Header" disabled={replaying}
                          style={{ flex: '0 0 180px', background: 'var(--panel)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '5px 8px', fontSize: 11, fontFamily: 'var(--mono)', outline: 'none' }} />
                        <input value={v} onChange={e => updateHeaderValue(idx, e.target.value)} placeholder="Value" disabled={replaying}
                          style={{ flex: 1, minWidth: 0, background: 'var(--panel)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '5px 8px', fontSize: 11, fontFamily: 'var(--mono)', outline: 'none' }} />
                        <button type="button" onClick={() => removeHeader(idx)} disabled={replaying}
                          style={{ background: 'none', border: '1px solid var(--border2)', color: '#ff4d4d88', padding: '4px 8px', fontSize: 11, cursor: replaying ? 'default' : 'pointer' }}>
                          <Icon d={Icons.trash} size={11} color="#ff4d4d" />
                        </button>
                      </div>
                    ))}
                    <button type="button" onClick={addHeader} disabled={replaying}
                      style={{ background: 'var(--panel)', border: '1px solid var(--border2)', color: 'var(--text-dim)', padding: '5px 10px', fontSize: 11, fontWeight: 600, cursor: replaying ? 'default' : 'pointer', letterSpacing: '.04em' }}>
                      + Add header
                    </button>
                  </div>
                ) : (
                  <div style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.7, overflowX: 'hidden' }}>
                    {Object.keys(selected.request_headers || {}).length > 0 ? Object.entries(selected.request_headers).map(([k,v]) => (
                      <div key={k} style={{ wordBreak: 'break-all', overflowWrap: 'anywhere' }}><span style={{color:'var(--text-dim)'}}>{k}: </span><span style={{color:'var(--text)'}}>{v}</span></div>
                    )) : (
                      <span style={{ color: 'var(--text-dim)' }}>No headers recorded.</span>
                    )}
                  </div>
                )}
              </div>

              <div style={{ marginBottom: 20 }}>
                <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)', marginBottom: 8 }}>Response Headers</div>
                <div style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.7, overflowX: 'hidden' }}>
                  {Object.keys(selected.response_headers || {}).length > 0 ? Object.entries(selected.response_headers).map(([k,v]) => (
                    <div key={k} style={{ wordBreak: 'break-all', overflowWrap: 'anywhere' }}><span style={{color:'var(--text-dim)'}}>{k}: </span><span style={{color:'var(--text)'}}>{v}</span></div>
                  )) : (
                    <span style={{ color: 'var(--text-dim)' }}>No headers recorded.</span>
                  )}
                </div>
              </div>

              <div style={{ marginBottom: 20 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>Request Body</div>
                  {selected.request_body_encoding === 'base64' && <Pill color="#f5c542">base64</Pill>}
                  {selected.request_body_truncated && <Pill color="#ff4d4d">truncated</Pill>}
                  {editMode && <Pill color="var(--accent)">editing</Pill>}
                </div>
                {editMode ? (
                  <textarea value={editBody} onChange={e => setEditBody(e.target.value)} disabled={replaying}
                    rows={8}
                    style={{ width: '100%', boxSizing: 'border-box', background: 'var(--panel2)', border: '1px solid var(--border)', color: 'var(--text)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.5, outline: 'none', resize: 'vertical' }} />
                ) : (
                  <pre style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.5, overflowX: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0, color: selected.request_body ? 'var(--text)' : 'var(--text-dim)', maxHeight: 280 }}>
                    {selected.request_body || 'Body capture disabled for this tunnel.'}
                  </pre>
                )}
              </div>

              <div style={{ marginBottom: 20 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>Response Body</div>
                  {selected.response_body_encoding === 'base64' && <Pill color="#f5c542">base64</Pill>}
                  {selected.response_body_truncated && <Pill color="#ff4d4d">truncated</Pill>}
                </div>
                <pre style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.5, overflowX: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0, color: selected.response_body ? 'var(--text)' : 'var(--text-dim)', maxHeight: 280 }}>
                  {selected.response_body || 'Body capture disabled for this tunnel.'}
                </pre>
              </div>

              {replayResult && (
                <div style={{ marginTop: 12, marginBottom: 20 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--accent)', marginBottom: 8 }}>Replay result</div>
                  {replayResult.error ? (
                    <div style={{ background: 'var(--panel2)', border: '1px solid var(--red)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--red)' }}>{replayResult.error}</div>
                  ) : (
                    <div>
                      <div style={{ display: 'flex', gap: 12, marginBottom: 8 }}>
                        <span style={{ fontFamily: 'var(--mono)', fontSize: 12, fontWeight: 600, color: statusColor(replayResult.status) }}>{replayResult.status}</span>
                        <span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)' }}>{replayResult.duration_ms}ms</span>
                        {replayResult.truncated && <Pill color="#ff4d4d">truncated</Pill>}
                      </div>
                      <pre style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.5, overflowX: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0, color: 'var(--text)', maxHeight: 280 }}>
                        {replayResult.body || '(empty response)'}
                      </pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          </>
        ) : (
          <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-dim)', fontSize: 13, flexDirection: 'column', gap: 8 }}>
            <Icon d={Icons.activity} size={32} color="var(--border2)" />
            <span>Select a request to inspect</span>
          </div>
        )}
      </div>
    </div>
  );
}

