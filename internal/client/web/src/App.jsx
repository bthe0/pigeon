import React, { useState, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { StatusDot } from './Shared';
import { Sidebar } from './Sidebar';
import { TunnelsView } from './TunnelsView';
import { InspectorView } from './InspectorView';
import { LogsView } from './LogsView';
import { SettingsView } from './SettingsView';

function latLonToXY(lat, lon, W, H) {
  return { x: (lon + 180) / 360 * W, y: (90 - lat) / 180 * H };
}

function flagFromCountryCode(code) {
  const cc = String(code || '').trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(cc)) return '🌐';
  return String.fromCodePoint(...cc.split('').map(c => 127397 + c.charCodeAt(0)));
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function WorldMap({ nodes, onHover, hoveredCity, W=420, H=210 }) {
  return (
    <div style={{ position:'relative', width:'100%', aspectRatio:`${W} / ${H}`, overflow:'hidden', border:'1px solid var(--border)', background:'linear-gradient(180deg, rgba(18,23,20,0.98), rgba(11,15,13,1))' }}>
      <img
        src="/world-map.png"
        alt=""
        style={{ position:'absolute', inset:0, width:'100%', height:'100%', objectFit:'cover', opacity:0.34, filter:'grayscale(1) brightness(0.7) contrast(1.15)' }}
      />
      <svg viewBox={`0 0 ${W} ${H}`} style={{ position:'absolute', inset:0, width:'100%', height:'100%', display:'block' }}>
        {[30,60,90,120,150].map(x => <line key={'vg'+x} x1={x/180*W} y1={0} x2={x/180*W} y2={H} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {[30,60,90,120,150,180,210,240,270,300,330].map(x => <line key={'vg2'+x} x1={x/360*W} y1={0} x2={x/360*W} y2={H} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {[H*0.25,H*0.5,H*0.75].map((y,i) => <line key={'hg'+i} x1={0} y1={y} x2={W} y2={y} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {nodes.map((n, i) => {
          const {x, y} = latLonToXY(n.lat, n.lon, W, H);
          const isHovered = hoveredCity === n.key;
          const r = Math.min(2.5 + n.users * 0.32, 6.5);
          return (
            <g key={n.key} onMouseEnter={() => onHover(n)} onMouseLeave={() => onHover(null)} style={{cursor:'pointer'}}>
              <circle cx={x} cy={y} r={r+4} fill="none" stroke="var(--accent)" strokeWidth="0.8" opacity="0.3">
                <animate attributeName="r" values={`${r+2};${r+10};${r+2}`} dur={`${2+i*0.3}s`} repeatCount="indefinite"/>
                <animate attributeName="opacity" values="0.4;0;0.4" dur={`${2+i*0.3}s`} repeatCount="indefinite"/>
              </circle>
              <circle cx={x} cy={y} r={r} fill={isHovered ? '#fff' : 'var(--accent)'} opacity={isHovered?1:0.9} />
              {n.users > 5 && <text x={x} y={y+0.5} textAnchor="middle" dominantBaseline="middle" fontSize="3.5" fontFamily="monospace" fontWeight="700" fill="#000">{n.users}</text>}
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function agoFromTime(timeString) {
  const t = new Date(timeString).getTime();
  if (!t) return 'just now';
  const diff = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (diff < 5) return 'just now';
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return `${Math.floor(diff / 3600)}h ago`;
}

function TunnelDetail({ tunnel, onClose }) {
  const [tab, setTab] = useState('details');
  const [hoveredCity, setHoveredCity] = useState(null);
  const [tooltip, setTooltip] = useState(null);
  const [visitors, setVisitors] = useState([]);

  useEffect(() => {
    if (!tunnel || tab !== 'visitors') return;
    const filter = tunnel.publicUrl || tunnel.id;
    const fetchVisitors = async () => {
      try {
        const res = await fetch(`/api/inspector?filter=${encodeURIComponent(filter)}`);
        if (!res.ok) throw new Error(await res.text());
        const raw = await res.json() || [];
        const mapped = [...raw].reverse().map((entry, index) => ({
          city: entry.city || 'Unknown location',
          country: entry.country || 'Unknown',
          countryCode: entry.country_code || '',
          flag: flagFromCountryCode(entry.country_code),
          lat: Number.isFinite(entry.latitude) ? entry.latitude : null,
          lon: Number.isFinite(entry.longitude) ? entry.longitude : null,
          browser: entry.browser || 'Unknown',
          os: entry.os || 'Unknown',
          ip: (entry.remote_addr || 'unknown').split(':')[0],
          ago: agoFromTime(entry.time),
          time: entry.time,
          durationMs: entry.duration_ms || 0,
          id: `${entry.time}-${entry.remote_addr}-${index}`,
        }));
        setVisitors(mapped.slice(0, 30));
      } catch (err) {
        setVisitors([]);
      }
    };
    fetchVisitors();
    const iv = setInterval(fetchVisitors, 1500);
    return () => clearInterval(iv);
  }, [tunnel, tab]);

  if (!tunnel) return null;

  const tabs = [{ id:'details', label:'Details' }, { id:'visitors', label:'Visitors' }];
  const nodes = Object.values(visitors.reduce((acc, visitor) => {
    if (visitor.lat == null || visitor.lon == null) return acc;
    const key = `${visitor.city}|${visitor.countryCode}|${visitor.lat}|${visitor.lon}`;
    if (!acc[key]) {
      acc[key] = {
        key,
        city: visitor.city,
        country: visitor.country,
        countryCode: visitor.countryCode,
        flag: visitor.flag,
        lat: visitor.lat,
        lon: visitor.lon,
        browser: visitor.browser,
        os: visitor.os,
        users: 0,
      };
    }
    acc[key].users += 1;
    return acc;
  }, {})).sort((a, b) => b.users - a.users);
  const activeWindowMs = 5 * 60 * 1000;
  const now = Date.now();
  const activeUsers = new Set(visitors.filter(v => {
    const t = new Date(v.time).getTime();
    return t && now-t <= activeWindowMs;
  }).map(v => v.ip)).size;
  const totalCountries = new Set(visitors.map(v => v.countryCode || v.country).filter(Boolean)).size;
  const avgDurationMs = visitors.length ? Math.round(visitors.reduce((sum, v) => sum + (v.durationMs || 0), 0) / visitors.length) : 0;
  const avgSession = avgDurationMs ? `${avgDurationMs}ms` : '—';

  function handleMapHover(node) {
    setHoveredCity(node ? node.key : null);
    setTooltip(node);
  }

  return (
    <div className="tunnel-detail-panel" style={{ position:'absolute', right:0, top:0, bottom:0, width: tab==='visitors' ? 520 : 360, background:'var(--panel)', borderLeft:'1px solid var(--border2)', display:'flex', flexDirection:'column', zIndex:50, animation:'slideIn .18s ease', transition:'width .2s ease' }}>
      <div style={{ padding:'14px 20px', borderBottom:'1px solid var(--border)', display:'flex', alignItems:'center', gap:10, flexShrink:0 }}>
        <StatusDot status={tunnel.status} />
        <span style={{ flex:1, fontSize:14, fontWeight:600, color:'#fff' }}>Local Target: {tunnel.localPort}</span>
        <button onClick={onClose} style={{ background:'none', border:'none', cursor:'pointer', color:'var(--text-dim)' }}><Icon d={Icons.x} size={16} color="currentColor" /></button>
      </div>

      <div style={{ display:'flex', borderBottom:'1px solid var(--border)', flexShrink:0 }}>
        {tabs.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            style={{ flex:1, padding:'8px 0', background:'none', border:'none', borderBottom:`2px solid ${tab===t.id?'var(--accent)':'transparent'}`, color:tab===t.id?'var(--accent)':'var(--text-dim)', fontSize:12, cursor:'pointer', fontFamily:'var(--sans)', fontWeight:tab===t.id?500:400, marginBottom:-1, transition:'all .12s' }}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'details' && (
        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
          {[
            ['ID', tunnel.id],
            ['Public Endpoint', tunnel.publicUrl ? `${tunnel.urlScheme}://${tunnel.publicUrl}` : 'auto-assigned (start daemon)'],
            ['Protocol', tunnel.proto.toUpperCase()],
            ['Status', tunnel.status],
            ['Latency', tunnel.latency ? `${tunnel.latency}ms` : '—'],
            ['Requests', tunnel.requests.toLocaleString()],
            ['Bandwidth', tunnel.bandwidth],
          ].map(([k,v]) => (
            <div key={k} style={{ marginBottom: 14 }}>
              <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)', marginBottom: 3 }}>{k}</div>
              <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text)', wordBreak: 'break-all' }}>{v}</div>
            </div>
          ))}
        </div>
      )}

      {tab === 'visitors' && (
        <div style={{ flex:1, display:'flex', flexDirection:'column', overflow:'hidden' }}>
          <div style={{ display:'flex', borderBottom:'1px solid var(--border)', flexShrink:0 }}>
            {[
              { label:'Active Users', value: activeUsers, accent:true },
              { label:'Countries', value: totalCountries },
              { label:'Avg Session', value: avgSession },
            ].map((s,i) => (
              <div key={i} style={{ flex:1, padding:'10px 14px', borderRight: i<2 ? '1px solid var(--border)' : 'none' }}>
                <div style={{ fontSize:10, fontWeight:600, letterSpacing:'.07em', textTransform:'uppercase', color:'var(--text-dim)' }}>{s.label}</div>
                <div style={{ fontFamily:'var(--mono)', fontSize:16, fontWeight:600, color: s.accent ? 'var(--accent)' : '#fff', marginTop:2 }}>{s.value}</div>
              </div>
            ))}
          </div>

          <div style={{ padding:'12px 16px 0', position:'relative', flexShrink:0 }}>
            <WorldMap nodes={nodes} onHover={handleMapHover} hoveredCity={hoveredCity} />
            {tooltip && (() => {
              const {x, y} = latLonToXY(tooltip.lat, tooltip.lon, 420, 210);
              const pct = x / 420;
              return (
                <div style={{ position:'absolute', top: `${(y/210)*100}%`, left:`calc(${pct*100}% + 8px)`, transform:'translateY(-50%)', background:'var(--panel2)', border:'1px solid var(--border2)', padding:'8px 10px', pointerEvents:'none', zIndex:10, minWidth:140 }}>
                  <div style={{ fontSize:12, fontWeight:600, color:'#fff', marginBottom:4 }}>{tooltip.flag} {tooltip.city}</div>
                  <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--accent)', marginBottom:2 }}>{tooltip.users} active users</div>
                  <div style={{ fontSize:10, color:'var(--text-dim)' }}>{tooltip.country || 'Unknown'}</div>
                </div>
              );
            })()}
          </div>

          <div style={{ flex:1, overflowY:'auto', borderTop:'1px solid var(--border)', marginTop:8 }}>
            <div style={{ padding:'6px 16px', display:'flex', alignItems:'center', justifyContent:'space-between', borderBottom:'1px solid var(--border)', flexShrink:0 }}>
              <span style={{ fontSize:10, fontWeight:600, letterSpacing:'.07em', textTransform:'uppercase', color:'var(--text-dim)' }}>Live Connections</span>
              <span style={{ display:'flex', alignItems:'center', gap:4, fontSize:10, color:'var(--accent)', fontFamily:'var(--mono)' }}>
                <span style={{ width:5, height:5, borderRadius:'50%', background:'var(--accent)', display:'inline-block', animation:'pulse 1.5s ease infinite' }}/>
                LIVE
              </span>
            </div>
            {visitors.length === 0 ? (
              <div style={{ padding:'20px 16px', color:'var(--text-dim)', fontSize:12 }}>No visitor data yet. Open the tunnel and generate a few requests.</div>
            ) : visitors.map((v, i) => (
              <div key={v.id || i} style={{ display:'grid', gridTemplateColumns:'26px 1fr 60px', gap:'0 8px', padding:'7px 16px', borderBottom:'1px solid var(--border)', alignItems:'center', background: i===0 ? 'var(--accent-dim)' : 'transparent', transition:'background .4s' }}>
                <span style={{ fontSize:14 }}>{v.flag}</span>
                <div>
                  <div style={{ fontSize:12, color:'#fff', fontWeight:500 }}>{v.city}
                    <span style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--text-dim)', marginLeft:6 }}>{v.ip}</span>
                  </div>
                  <div style={{ fontSize:10, color:'var(--text-dim)', marginTop:1 }}>{v.browser} · {v.os}</div>
                </div>
                <div style={{ textAlign:'right', fontFamily:'var(--mono)', fontSize:10, color: i===0?'var(--accent)':'var(--text-dim)' }}>{v.ago}</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function StatsBar({ tunnels, server, version }) {
  const online = tunnels.length;
  const totalReqs = tunnels.reduce((a, t) => a + t.requests, 0);
  return (
    <div className="stats-bar" style={{ display: 'flex', gap: 0, borderBottom: '1px solid var(--border)', flexShrink: 0, background: 'var(--panel)' }}>
      {[
        { label: 'Active Tunnels', value: `${online} connected`, accent: true },
        { label: 'Total Mocks', value: totalReqs.toLocaleString() },
        { label: 'Agent', value: version || 'dev' },
        { label: 'Pigeon Server', value: server || 'Unknown Server' },
      ].map((s, i) => (
        <div key={i} style={{ padding: '8px 24px', borderRight: '1px solid var(--border)', minWidth: 120 }}>
          <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>{s.label}</div>
          <div style={{ fontFamily: 'var(--mono)', fontSize: 13, fontWeight: 500, color: s.accent ? 'var(--accent)' : 'var(--text)', marginTop: 2 }}>{s.value}</div>
        </div>
      ))}
    </div>
  );
}

function metricFromID(id, min, max) {
  const s = String(id || '');
  let hash = 0;
  for (let i = 0; i < s.length; i++) hash = ((hash * 31) + s.charCodeAt(i)) >>> 0;
  return min + (hash % (max - min + 1));
}

const NAV_PAGES = ['tunnels', 'inspector', 'logs', 'settings'];
function hashNav() {
  const h = window.location.hash.replace('#', '');
  return NAV_PAGES.includes(h) ? h : 'tunnels';
}

export function App() {
  const [activeNav, setActiveNav] = useState(hashNav);
  const [tunnels, setTunnels] = useState([]);
  const [rawConfig, setRawConfig] = useState(null);
  const [selectedTunnel, setSelectedTunnel] = useState(null);
  const [initError, setInitError] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const onHash = () => setActiveNav(hashNav());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  useEffect(() => {
    const label = activeNav.charAt(0).toUpperCase() + activeNav.slice(1);
    document.title = `Pigeon | ${label}`;
  }, [activeNav]);

  const loadConfig = async () => {
    try {
      const res = await fetch('/api/config');
      if(!res.ok) {
        const txt = await res.text();
        if(txt.includes('not initialised')) setInitError(txt);
        throw new Error(txt);
      }
      setInitError(null);
      const cfg = await res.json();
      setRawConfig(cfg);
      
      const isLocal = !!cfg.local_dev;
      const baseDomain = cfg.base_domain || '';
      const parsedTunnels = (cfg.forwards || []).map(f => {
        let pubUrl = '';
        let urlScheme = 'https';
        const expose = f.expose || 'both';
        if (f.protocol === 'http' || f.protocol === 'https') {
          let raw = f.public_addr || f.domain || (baseDomain ? `${f.id}.${baseDomain}` : null);
          // if the stored value has no dot (e.g. a short label without base domain), append it
          if (raw && baseDomain && !raw.endsWith('.' + baseDomain) && raw !== baseDomain) raw = `${raw}.${baseDomain}`;
          pubUrl = raw;
          urlScheme = expose === 'http' ? 'http' : 'https';
        } else {
          pubUrl = f.public_addr || (f.remote_port > 0 ? `Port ${f.remote_port}` : null);
          urlScheme = f.protocol;
        }

        return {
          id: f.id,
          name: f.id,
          proto: f.protocol,
          localPort: f.local_addr,
          publicUrl: pubUrl,
          urlScheme,
          isLocal,
          status: f.disabled ? 'offline' : 'online',
          disabled: f.disabled,
          domain: f.domain,
          remotePort: f.remote_port,
          expose: f.expose || 'both',
          httpPassword: f.http_password || '',
          maxConnections: f.max_connections || 0,
          unavailablePage: f.unavailable_page || 'default',
          region: 'auto',
          requests: f.requests || 0,
          latency: f.disabled ? null : metricFromID(f.id, 8, 95),
          bandwidth: formatBytes(f.bytes || 0),
          tags: [f.protocol]
        };
      });
      setTunnels(parsedTunnels);

    } catch(err) {
      console.error("Config fetch error", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
  }, []);

  if (initError) {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg)', color: '#fff', flexDirection: 'column', gap: 20 }}>
        <Icon d={Icons.zap} size={48} color="var(--accent)" />
        <div style={{ fontSize: 24, fontWeight: 600 }}>Daemon Not Initialized</div>
        <div style={{ color: 'var(--text-mid)', fontFamily: 'var(--mono)', fontSize: 13, background: 'var(--panel)', padding: '16px 24px', border: '1px solid var(--border)' }}>
          {initError}
        </div>
        <button onClick={loadConfig} style={{ background: 'none', border: '1px solid var(--border2)', color: 'var(--text-dim)', padding: '8px 16px', cursor: 'pointer', marginTop: 10 }}>Retry Connection</button>
      </div>
    );
  }

  return (
    <div className="app-layout" style={{ height: '100vh', display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
      <StatsBar tunnels={tunnels} server={rawConfig?.server} version={rawConfig?.version} />
      <div className="app-layout" style={{ flex: 1, display: 'flex', overflow: 'hidden', position: 'relative' }}>
        <Sidebar active={activeNav} setActive={v => { window.location.hash = v; setSelectedTunnel(null); }} />
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
          {activeNav === 'tunnels' && <TunnelsView tunnels={tunnels} loading={loading} reloadConfig={loadConfig} onSelectTunnel={t => setSelectedTunnel(t)} baseDomain={rawConfig?.base_domain || ''} />}
          {activeNav === 'inspector' && <InspectorView tunnels={tunnels} />}
          {activeNav === 'logs' && <LogsView />}
          {activeNav === 'settings' && <SettingsView config={rawConfig} loading={loading} />}
          {selectedTunnel && activeNav === 'tunnels' && <TunnelDetail tunnel={selectedTunnel} onClose={() => setSelectedTunnel(null)} />}
        </div>
      </div>
    </div>
  );
}

