import React, { useState, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { Pill } from './Shared';
import { statusColor } from './Constants';

const METHOD_COLORS = { GET:'#4d9fff', POST:'#00e87a', PUT:'#f5c542', DELETE:'#ff4d4d', PATCH:'#c084fc', OPTIONS:'#6b7068', HEAD:'#9ba39c' };

export function InspectorView({ tunnels }) {
  const [selected, setSelected] = useState(null);
  const [requests, setRequests] = useState([]);
  const [filterTunnel, setFilterTunnel] = useState('all');
  const [liveMode, setLiveMode] = useState(true);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!liveMode) return;
    const fetchInspector = async () => {
      try {
        const qs = filterTunnel === 'all' ? '' : `?filter=${encodeURIComponent(filterTunnel)}`;
        const res = await fetch(`/api/inspector${qs}`);
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
                <span style={{ fontFamily: 'var(--mono)', fontSize: 13, color: '#fff' }}>{selected.path}</span>
                <span style={{ fontFamily: 'var(--mono)', fontSize: 12, fontWeight: 600, color: statusColor(selected.status) }}>{selected.status}</span>
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
              {[
                ['Request Headers', selected.request_headers || {}],
                ['Response Headers', selected.response_headers || {}],
              ].map(([title, headers]) => (
                <div key={title} style={{ marginBottom: 20 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)', marginBottom: 8 }}>{title}</div>
                  <div style={{ background: 'var(--panel2)', border: '1px solid var(--border)', padding: '12px 14px', fontFamily: 'var(--mono)', fontSize: 11, lineHeight: 1.7, overflowX: 'hidden' }}>
                    {Object.keys(headers).length > 0 ? Object.entries(headers).map(([k,v]) => (
                      <div key={k} style={{ wordBreak: 'break-all', overflowWrap: 'anywhere' }}><span style={{color:'var(--text-dim)'}}>{k}: </span><span style={{color:'var(--text)'}}>{v}</span></div>
                    )) : (
                      <span style={{ color: 'var(--text-dim)' }}>No headers recorded.</span>
                    )}
                  </div>
                </div>
              ))}
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

