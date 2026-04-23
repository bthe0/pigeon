import { useState, useEffect, useRef } from 'react';
import { Icon, Icons } from './Icons';
import { StatusDot } from './Shared';
import { Sidebar } from './Sidebar';
import { TunnelsView } from './TunnelsView';
import { InspectorView } from './InspectorView';
import { LogsView } from './LogsView';
import { SettingsView } from './SettingsView';
import { LoginView } from './LoginView';
import styles from './App.module.css';

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

// State-changing methods trigger the server's CSRF guard: browsers can't set
// custom headers cross-origin without a CORS preflight (which the dashboard
// doesn't answer), so requiring X-Pigeon-CSRF blocks form-POST style attacks
// even if SameSite=Lax is bypassed.
const CSRF_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

const dashFetch = async (url, opts = {}, onUnauthorized) => {
  const method = (opts.method || 'GET').toUpperCase();
  const headers = new Headers(opts.headers || {});
  if (CSRF_METHODS.has(method) && !headers.has('X-Pigeon-CSRF')) {
    headers.set('X-Pigeon-CSRF', '1');
  }
  const res = await fetch(url, { ...opts, headers });
  if (res.status === 401 && onUnauthorized) {
    onUnauthorized();
  }
  return res;
};

function WorldMap({ nodes, onHover, hoveredCity, W = 420, H = 210 }) {
  return (
    <div className={styles.worldMap} style={{ aspectRatio: `${W} / ${H}` }}>
      <img src="/world-map.png" alt="" className={styles.worldMapImg} />
      <svg viewBox={`0 0 ${W} ${H}`} className={styles.worldMapSvg}>
        {[30,60,90,120,150].map(x => <line key={'vg'+x} x1={x/180*W} y1={0} x2={x/180*W} y2={H} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {[30,60,90,120,150,180,210,240,270,300,330].map(x => <line key={'vg2'+x} x1={x/360*W} y1={0} x2={x/360*W} y2={H} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {[H*0.25,H*0.5,H*0.75].map((y,i) => <line key={'hg'+i} x1={0} y1={y} x2={W} y2={y} stroke="rgba(255,255,255,0.04)" strokeWidth="0.5" />)}
        {nodes.map((n, i) => {
          const { x, y } = latLonToXY(n.lat, n.lon, W, H);
          const isHovered = hoveredCity === n.key;
          const r = Math.min(2.5 + n.users * 0.32, 6.5);
          return (
            <g key={n.key} onMouseEnter={() => onHover(n)} onMouseLeave={() => onHover(null)} className={styles.worldNode}>
              <circle cx={x} cy={y} r={r + 4} fill="none" stroke="var(--accent)" strokeWidth="0.8" opacity="0.3">
                <animate attributeName="r" values={`${r+2};${r+10};${r+2}`} dur={`${2+i*0.3}s`} repeatCount="indefinite" />
                <animate attributeName="opacity" values="0.4;0;0.4" dur={`${2+i*0.3}s`} repeatCount="indefinite" />
              </circle>
              <circle cx={x} cy={y} r={r} fill={isHovered ? '#fff' : 'var(--accent)'} opacity={isHovered ? 1 : 0.9} />
              {n.users > 5 && <text x={x} y={y + 0.5} textAnchor="middle" dominantBaseline="middle" fontSize="3.5" fontFamily="monospace" fontWeight="700" fill="#000">{n.users}</text>}
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

// ── TunnelCharts ─────────────────────────────────────────────────────────────
// Lightweight SVG charts rendered from the last ~100 inspector entries for a
// tunnel. Designed to give at-a-glance insight — request rate, HTTP-status
// mix, and latency timeline — without pulling in a chart library.

const STATUS_CLASS = (s) =>
  s >= 500 ? '5xx' :
  s >= 400 ? '4xx' :
  s >= 300 ? '3xx' :
  s >= 200 ? '2xx' : '1xx';

const STATUS_CLASS_COLORS = {
  '2xx': '#00e87a',
  '3xx': '#4d9fff',
  '4xx': '#f5c542',
  '5xx': '#ff4d4d',
  '1xx': '#9ba39c',
};

function TunnelCharts({ entries }) {
  if (!entries || entries.length === 0) {
    return (
      <div className={styles.chartsPlaceholder}>
        No request data yet. Once traffic hits the tunnel, charts will appear here.
      </div>
    );
  }

  const parsed = entries.map(e => ({
    ts: new Date(e.time).getTime(),
    status: e.status || 0,
    ms: e.duration_ms || 0,
    bytes: e.bytes || 0,
    path: e.path || '/',
    method: e.method || 'GET',
  })).filter(e => e.ts > 0).sort((a, b) => a.ts - b.ts);

  const now = Date.now();
  const total = parsed.length;
  const avgLatency = parsed.reduce((s, e) => s + e.ms, 0) / total;
  const p95Latency = percentile(parsed.map(e => e.ms), 95);
  const errors = parsed.filter(e => e.status >= 400).length;
  const errorRate = total > 0 ? (errors / total) * 100 : 0;
  const totalBytes = parsed.reduce((s, e) => s + e.bytes, 0);

  const bins = 20;
  const windowMs = 10 * 60 * 1000;
  const binMs = windowMs / bins;
  const start = now - windowMs;
  const perBin = Array.from({ length: bins }, () => ({ count: 0, errors: 0 }));
  parsed.forEach(e => {
    if (e.ts < start) return;
    const idx = Math.min(bins - 1, Math.floor((e.ts - start) / binMs));
    perBin[idx].count++;
    if (e.status >= 400) perBin[idx].errors++;
  });
  const maxCount = Math.max(1, ...perBin.map(b => b.count));

  const classes = { '2xx': 0, '3xx': 0, '4xx': 0, '5xx': 0, '1xx': 0 };
  parsed.forEach(e => { classes[STATUS_CLASS(e.status)] = (classes[STATUS_CLASS(e.status)] || 0) + 1; });
  const classEntries = Object.entries(classes).filter(([, v]) => v > 0);

  const byPath = {};
  parsed.forEach(e => {
    const key = `${e.method} ${e.path}`;
    byPath[key] = (byPath[key] || 0) + 1;
  });
  const topPaths = Object.entries(byPath).sort((a, b) => b[1] - a[1]).slice(0, 5);

  return (
    <div className={styles.chartsBody}>
      <div className={styles.statCards}>
        <StatCard label="Requests"    value={total.toLocaleString()} />
        <StatCard label="Avg Latency" value={`${Math.round(avgLatency)}ms`} />
        <StatCard label="p95 Latency" value={`${Math.round(p95Latency)}ms`} accent={p95Latency > 500 ? '#f5c542' : undefined} />
        <StatCard label="Error Rate"  value={`${errorRate.toFixed(1)}%`} accent={errorRate > 5 ? '#ff4d4d' : '#00e87a'} />
      </div>

      <ChartCard title="Requests · last 10 min" right={`${formatBytesShort(totalBytes)} transferred`}>
        <RequestRateChart bins={perBin} max={maxCount} />
      </ChartCard>

      <ChartCard title="Latency" right={`avg ${Math.round(avgLatency)}ms · p95 ${Math.round(p95Latency)}ms`}>
        <LatencyChart points={parsed.slice(-60)} />
      </ChartCard>

      <ChartCard title="Status codes" right={`${total} samples`}>
        <StatusBreakdown entries={classEntries} total={total} />
      </ChartCard>

      <ChartCard title="Top paths">
        {topPaths.length === 0 ? (
          <div className={styles.chartEmpty}>No HTTP activity yet.</div>
        ) : topPaths.map(([k, n]) => (
          <div key={k} className={styles.topPathRow}>
            <span className={styles.topPathKey}>{k}</span>
            <div className={styles.topPathBar}>
              <div className={styles.topPathFill} style={{ width: `${(n / topPaths[0][1]) * 100}%` }} />
            </div>
            <span className={styles.topPathCount}>{n}</span>
          </div>
        ))}
      </ChartCard>
    </div>
  );
}

function StatCard({ label, value, accent }) {
  return (
    <div className={styles.statCard}>
      <div className={styles.statCardLabel}>{label}</div>
      <div className={styles.statCardValue} style={accent ? { color: accent } : undefined}>{value}</div>
    </div>
  );
}

function ChartCard({ title, right, children }) {
  return (
    <div className={styles.chartCard}>
      <div className={styles.chartCardHeader}>
        <span className={styles.chartCardTitle}>{title}</span>
        {right && <span className={styles.chartCardRight}>{right}</span>}
      </div>
      {children}
    </div>
  );
}

function RequestRateChart({ bins, max }) {
  const W = 488, H = 60, gap = 2;
  const bw = (W - gap * (bins.length - 1)) / bins.length;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className={styles.chartSvg} style={{ height: H }}>
      {bins.map((b, i) => {
        const h = b.count === 0 ? 0 : Math.max(2, (b.count / max) * (H - 4));
        const errorH = b.errors === 0 ? 0 : Math.max(1, (b.errors / max) * (H - 4));
        const x = i * (bw + gap);
        const y = H - h;
        const color = b.errors > 0 ? '#ff4d4d' : 'var(--accent)';
        return (
          <g key={i}>
            <rect x={x} y={y} width={bw} height={h} fill={color} opacity={b.count === 0 ? 0.15 : 0.8} />
            {b.errors > 0 && <rect x={x} y={H - errorH} width={bw} height={errorH} fill="#ff4d4d" />}
          </g>
        );
      })}
    </svg>
  );
}

function LatencyChart({ points }) {
  if (points.length < 2) {
    return <div className={styles.chartEmpty}>Need more samples to plot.</div>;
  }
  const W = 488, H = 80;
  const xs = points.map((_, i) => (i / (points.length - 1)) * W);
  const max = Math.max(1, ...points.map(p => p.ms));
  const ys = points.map(p => H - (p.ms / max) * (H - 4) - 2);
  const d = points.map((_, i) => `${i === 0 ? 'M' : 'L'} ${xs[i]} ${ys[i]}`).join(' ');
  const fill = `${d} L ${W} ${H} L 0 ${H} Z`;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className={styles.chartSvg} style={{ height: H }}>
      <path d={fill} fill="var(--accent)" opacity="0.15" />
      <path d={d} fill="none" stroke="var(--accent)" strokeWidth="1.5" />
      {points.map((p, i) => (
        <circle key={i} cx={xs[i]} cy={ys[i]} r={p.status >= 500 ? 2.5 : p.status >= 400 ? 2 : 1.5}
          fill={STATUS_CLASS_COLORS[STATUS_CLASS(p.status)] || '#9ba39c'} />
      ))}
    </svg>
  );
}

function StatusBreakdown({ entries, total }) {
  return (
    <div className={styles.statusBreakdown}>
      <div className={styles.statusBar}>
        {entries.map(([k, v]) => (
          <div key={k} title={`${k}: ${v}`} style={{ width: `${(v / total) * 100}%`, background: STATUS_CLASS_COLORS[k] || '#9ba39c' }} />
        ))}
      </div>
      <div className={styles.statusLegend}>
        {entries.map(([k, v]) => (
          <div key={k} className={styles.statusLegendItem}>
            <span className={styles.statusSwatch} style={{ background: STATUS_CLASS_COLORS[k] || '#9ba39c' }} />
            <span className={styles.statusKey}>{k}</span>
            <span className={styles.statusCount}>{v}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function percentile(sorted, p) {
  if (sorted.length === 0) return 0;
  const arr = [...sorted].sort((a, b) => a - b);
  const idx = Math.min(arr.length - 1, Math.floor((p / 100) * arr.length));
  return arr[idx];
}

function formatBytesShort(n) {
  if (!n) return '0 B';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

function TunnelDetail({ tunnel, onClose, dashFetch }) {
  const [tab, setTab] = useState('details');
  const [hoveredCity, setHoveredCity] = useState(null);
  const [tooltip, setTooltip] = useState(null);
  const [visitors, setVisitors] = useState([]);
  const [entries, setEntries] = useState([]);
  const panelRef = useRef(null);

  useEffect(() => {
    if (!tunnel) return;
    const onDown = (e) => {
      if (panelRef.current && !panelRef.current.contains(e.target)) {
        onClose();
      }
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [tunnel, onClose]);

  useEffect(() => {
    if (!tunnel || (tab !== 'visitors' && tab !== 'details')) return;
    const filter = tunnel.publicUrl || tunnel.id;
    const poll = async () => {
      try {
        const res = await dashFetch(`/api/inspector?filter=${encodeURIComponent(filter)}`);
        if (!res.ok) throw new Error(await res.text());
        const raw = await res.json() || [];
        setEntries(raw);
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
        setEntries([]);
      }
    };
    poll();
    const iv = setInterval(poll, 1500);
    return () => clearInterval(iv);
  }, [tunnel, tab]);

  if (!tunnel) return null;

  const tabs = [
    { id: 'details', label: 'Details' },
    { id: 'visitors', label: 'Visitors' },
  ];
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
    return t && now - t <= activeWindowMs;
  }).map(v => v.ip)).size;
  const totalCountries = new Set(visitors.map(v => v.countryCode || v.country).filter(Boolean)).size;
  const avgDurationMs = visitors.length ? Math.round(visitors.reduce((sum, v) => sum + (v.durationMs || 0), 0) / visitors.length) : 0;
  const avgSession = avgDurationMs ? `${avgDurationMs}ms` : '—';

  function handleMapHover(node) {
    setHoveredCity(node ? node.key : null);
    setTooltip(node);
  }

  return (
    <div ref={panelRef} className={styles.detailPanel}>
      <div className={styles.detailHeader}>
        <StatusDot status={tunnel.status} />
        <span className={styles.detailTitle}>Local Target: {tunnel.localPort}</span>
        <button onClick={onClose} className={styles.closeBtn}>
          <Icon d={Icons.x} size={16} color="currentColor" />
        </button>
      </div>

      <div className={styles.tabRow}>
        {tabs.map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`${styles.tabBtn} ${tab === t.id ? styles.tabBtnActive : ''}`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'details' && (
        <div className={styles.detailBody}>
          <div className={styles.metadataGrid}>
            {[
              ['ID', tunnel.id],
              ['Protocol', tunnel.proto.toUpperCase()],
              ['Status', tunnel.status],
              ['Latency', tunnel.latency ? `${tunnel.latency}ms` : '—'],
              ['Requests', tunnel.requests.toLocaleString()],
              ['Bandwidth', tunnel.bandwidth],
            ].map(([k, v]) => (
              <div key={k}>
                <div className={styles.metaLabel}>{k}</div>
                <div className={styles.metaValue}>{v}</div>
              </div>
            ))}
            <div className={styles.metaFull}>
              <div className={styles.metaLabel}>Public Endpoint</div>
              <div className={styles.metaValue}>
                {tunnel.publicUrl ? (
                  <a
                    href={`${tunnel.urlScheme}://${tunnel.publicUrl}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.endpointLink}
                  >
                    {`${tunnel.urlScheme}://${tunnel.publicUrl}`}
                  </a>
                ) : 'auto-assigned (start daemon)'}
              </div>
            </div>
          </div>

          <TunnelCharts entries={entries} />
        </div>
      )}

      {tab === 'visitors' && (
        <div className={styles.visitorsTab}>
          <div className={styles.visitorStats}>
            {[
              { label: 'Active Users', value: activeUsers, accent: true },
              { label: 'Countries', value: totalCountries },
              { label: 'Avg Session', value: avgSession },
            ].map((s, i) => (
              <div key={i} className={`${styles.visitorStatCell} ${i < 2 ? styles.visitorStatCellBorder : ''}`}>
                <div className={styles.visitorStatLabel}>{s.label}</div>
                <div className={`${styles.visitorStatValue} ${s.accent ? styles.visitorStatAccent : ''}`}>{s.value}</div>
              </div>
            ))}
          </div>

          <div className={styles.mapWrap}>
            <WorldMap nodes={nodes} onHover={handleMapHover} hoveredCity={hoveredCity} />
            {tooltip && (() => {
              const { x, y } = latLonToXY(tooltip.lat, tooltip.lon, 420, 210);
              const pct = x / 420;
              return (
                <div
                  className={styles.mapTooltip}
                  style={{
                    top: `${(y / 210) * 100}%`,
                    left: `calc(${pct * 100}% + 8px)`,
                  }}
                >
                  <div className={styles.tooltipTitle}>{tooltip.flag} {tooltip.city}</div>
                  <div className={styles.tooltipCount}>{tooltip.users} active users</div>
                  <div className={styles.tooltipSub}>{tooltip.country || 'Unknown'}</div>
                </div>
              );
            })()}
          </div>

          <div className={styles.visitorsList}>
            <div className={styles.visitorsListHeader}>
              <span className={styles.visitorsListHeaderLabel}>Live Connections</span>
              <span className={styles.liveLabel}>
                <span className={styles.liveDotSmall} />
                LIVE
              </span>
            </div>
            {visitors.length === 0 ? (
              <div className={styles.visitorsEmpty}>No visitor data yet. Open the tunnel and generate a few requests.</div>
            ) : visitors.map((v, i) => (
              <div
                key={v.id || i}
                className={`${styles.visitorRow} ${i === 0 ? styles.visitorRowFresh : ''}`}
              >
                <span className={styles.visitorFlag}>{v.flag}</span>
                <div>
                  <div className={styles.visitorName}>
                    {v.city}
                    <span className={styles.visitorIp}>{v.ip}</span>
                  </div>
                  <div className={styles.visitorAgent}>{v.browser} · {v.os}</div>
                </div>
                <div className={`${styles.visitorAgo} ${i === 0 ? styles.visitorAgoFresh : ''}`}>{v.ago}</div>
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
  const cells = [
    { label: 'Active Tunnels', value: `${online} connected`, accent: true },
    { label: 'Total Mocks', value: totalReqs.toLocaleString() },
    { label: 'Agent', value: version || 'dev' },
    { label: 'Pigeon Server', value: server || 'Unknown Server' },
  ];
  return (
    <div className={styles.statsBar}>
      {cells.map((s, i) => (
        <div key={i} className={styles.statCell}>
          <div className={styles.statLabel}>{s.label}</div>
          <div className={`${styles.statValue} ${s.accent ? styles.statValueAccent : ''}`}>{s.value}</div>
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
  const [isAuthorized, setIsAuthorized] = useState(null);
  const [initError, setInitError] = useState(null);
  const [loading, setLoading] = useState(true);

  const wrappedFetch = (url, opts) => dashFetch(url, opts, () => setIsAuthorized(false));

  const handleLogout = async () => {
    await fetch('/api/logout', { method: 'POST' });
    setIsAuthorized(false);
  };

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
      const res = await wrappedFetch('/api/config');
      if (!res.ok) {
        const txt = await res.text();
        if (txt.includes('not initialised')) setInitError(txt);
        throw new Error(txt);
      }
      setInitError(null);
      setIsAuthorized(true);
      const cfg = await res.json();
      setRawConfig(cfg);

      const isLocal = !!cfg.local_dev;
      const baseDomain = cfg.base_domain || '';
      const parsedTunnels = (cfg.forwards || []).map(f => {
        let pubUrl = '';
        let urlScheme = 'https';
        const expose = f.expose || 'both';
        const httpLike = f.protocol === 'http' || f.protocol === 'https' || f.protocol === 'static';
        if (httpLike) {
          let raw = f.public_addr || f.domain || null;
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
          localPort: f.protocol === 'static' ? (f.static_root || '') : f.local_addr,
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
          allowedIPs: f.allowed_ips || [],
          captureBodies: !!f.capture_bodies,
          staticRoot: f.static_root || '',
          region: 'auto',
          requests: f.requests || 0,
          latency: f.disabled ? null : metricFromID(f.id, 8, 95),
          bandwidth: formatBytes(f.bytes || 0),
          tags: [f.protocol]
        };
      });
      setTunnels(parsedTunnels);

    } catch (err) {
      console.error("Config fetch error", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
  }, []);

  if (isAuthorized === null) return null;

  if (!isAuthorized) {
    return <LoginView onLogin={() => { setIsAuthorized(true); loadConfig(); }} />;
  }

  if (initError) {
    return (
      <div className={styles.initScreen}>
        <Icon d={Icons.zap} size={48} color="var(--accent)" />
        <div className={styles.initTitle}>Daemon Not Initialized</div>
        <div className={styles.initMessage}>{initError}</div>
        <button onClick={loadConfig} className={styles.retryBtn}>Retry Connection</button>
      </div>
    );
  }

  return (
    <div className={styles.appLayout}>
      <StatsBar tunnels={tunnels} server={rawConfig?.server} version={rawConfig?.version} />
      <div className={styles.appBody}>
        <Sidebar active={activeNav} setActive={v => { window.location.hash = v; setSelectedTunnel(null); }} onLogout={handleLogout} />
        <div className={styles.appMain}>
          {activeNav === 'tunnels' && <TunnelsView tunnels={tunnels} loading={loading} reloadConfig={loadConfig} onSelectTunnel={t => setSelectedTunnel(t)} baseDomain={rawConfig?.base_domain || ''} dashFetch={wrappedFetch} />}
          {activeNav === 'inspector' && <InspectorView tunnels={tunnels} dashFetch={wrappedFetch} />}
          {activeNav === 'logs' && <LogsView dashFetch={wrappedFetch} />}
          {activeNav === 'settings' && <SettingsView config={rawConfig} loading={loading} dashFetch={wrappedFetch} />}
          {selectedTunnel && activeNav === 'tunnels' && <TunnelDetail tunnel={selectedTunnel} onClose={() => setSelectedTunnel(null)} dashFetch={wrappedFetch} />}
        </div>
      </div>
    </div>
  );
}
