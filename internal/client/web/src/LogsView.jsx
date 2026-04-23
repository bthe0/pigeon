import { useState, useEffect, useRef } from 'react';
import styles from './LogsView.module.css';

// ── Palette ──────────────────────────────────────────────────────────────────
// Each log "kind" gets a distinct colour so the stream is scannable at a glance.
const KIND_COLORS = {
  INFO:  '#9ba39c',
  OK:    '#00e87a',
  WARN:  '#f5c542',
  ERROR: '#ff4d4d',
  DEBUG: '#9b8fff',
  HTTP:  '#4d9fff',
  TCP:   '#00e87a',
  UDP:   '#c084fc',
  SYS:   '#00e87a',
};

const METHOD_COLORS = {
  GET: '#4d9fff', POST: '#00e87a', PUT: '#f5c542',
  DELETE: '#ff4d4d', PATCH: '#c084fc', OPTIONS: '#6b7068', HEAD: '#9ba39c',
};

const statusColor = (s) =>
  s >= 500 ? '#ff4d4d' :
  s >= 400 ? '#f5c542' :
  s >= 300 ? '#4d9fff' :
  s >= 200 ? '#00e87a' :
             '#9ba39c';

const DAEMON_RULES = [
  { kind: 'ERROR', match: /\berror\b|\bfail(ed)?\b|\brefus(ed|al)\b|dial local|cannot |panic/i },
  { kind: 'WARN',  match: /\bwarn(ing)?\b|disconnect|reconnect|retry|attempt|rate[- ]limit|rejected|locked/i },
  { kind: 'OK',    match: /\bconnected\b|forward ready|dashboard at|web ui|listening on|tunnel ready|enabled|started/i },
  { kind: 'DEBUG', match: /attempt \d|\bping\b|\bpong\b|reloaded|skipping|saving/i },
];

function classifyDaemon(msg) {
  for (const r of DAEMON_RULES) if (r.match.test(msg)) return r.kind;
  return 'INFO';
}

function parseHTTPAction(action) {
  const m = action && action.match(/^(\w+)\s+(\S+)\s+(\d{3})\s+(\d+)ms$/);
  if (!m) return null;
  return { method: m[1], path: m[2], status: parseInt(m[3], 10), ms: parseInt(m[4], 10) };
}

function classifyRow(r) {
  const d = new Date(r.time);
  const t = `${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}:${String(d.getSeconds()).padStart(2,'0')}`;

  const proto = (r.protocol || '').toUpperCase();
  const action = r.action || '';

  if (proto === 'DAEMON') {
    return { t, kind: classifyDaemon(action), type: 'sys', msg: action };
  }

  const tunnel = r.domain || r.forward_id || '';
  const remote = r.remote_addr || '';
  const bytes  = r.bytes || 0;

  if (proto === 'HTTP' || proto === 'HTTPS') {
    const parsed = parseHTTPAction(action);
    if (parsed) {
      const kind = parsed.status >= 500 ? 'ERROR' : parsed.status >= 400 ? 'WARN' : 'HTTP';
      return { t, kind, type: 'http', parsed, tunnel, remote, bytes };
    }
    return { t, kind: 'HTTP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  if (proto === 'TCP') {
    return { t, kind: 'TCP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  if (proto === 'UDP') {
    return { t, kind: 'UDP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  if (tunnel || remote || bytes) {
    return { t, kind: 'INFO', type: 'event', action: action || 'request', tunnel, remote, bytes, proto: proto || '' };
  }
  return { t, kind: 'INFO', type: 'sys', msg: `${r.protocol || '?'} ${action}`.trim() };
}

export function LogsView({ dashFetch }) {
  const [logs, setLogs] = useState([]);
  const [live, setLive] = useState(true);
  const [kindFilter, setKindFilter] = useState('ALL');
  const [loading, setLoading] = useState(true);
  const scrollRef = useRef(null);
  const stickToBottomRef = useRef(true);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    };
    onScroll();
    el.addEventListener('scroll', onScroll);
    return () => el.removeEventListener('scroll', onScroll);
  }, []);

  useEffect(() => {
    if (!live) return;
    const fetchLogs = async () => {
      try {
        const res = await dashFetch('/api/logs');
        if (!res.ok) { setLoading(false); return; }
        const raw = await res.json() || [];
        setLogs(raw.map(classifyRow));
      } catch (err) { /* keep last-known logs */ }
      setLoading(false);
    };
    fetchLogs();
    const iv = setInterval(fetchLogs, 1000);
    return () => clearInterval(iv);
  }, [live]);

  useEffect(() => {
    if (loading || !stickToBottomRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    requestAnimationFrame(() => { el.scrollTop = el.scrollHeight; });
  }, [loading, logs, kindFilter]);

  const filters = ['ALL', 'HTTP', 'TCP', 'UDP', 'OK', 'WARN', 'ERROR'];
  const filtered = kindFilter === 'ALL'
    ? logs
    : logs.filter(l => {
        if (l.kind === kindFilter) return true;
        if (kindFilter === 'HTTP' && (l.type === 'http' || (l.type === 'event' && (l.proto === 'HTTP' || l.proto === 'HTTPS')))) return true;
        if (kindFilter === 'TCP' && l.type === 'event' && l.proto === 'TCP') return true;
        if (kindFilter === 'UDP' && l.type === 'event' && l.proto === 'UDP') return true;
        return false;
      });

  return (
    <div className={styles.page}>
      <div className={styles.toolbar}>
        <div className={styles.toolbarTitleWrap}>
          <div className={styles.toolbarTitle}>System Logs</div>
          <div className={styles.toolbarCount}>{filtered.length} entries</div>
        </div>
        <div className={styles.filters}>
          {filters.map(l => {
            const active = kindFilter === l;
            const c = KIND_COLORS[l] || 'var(--accent)';
            return (
              <button
                key={l}
                onClick={() => setKindFilter(l)}
                className={styles.filterBtn}
                style={active ? { background: c + '22', borderColor: c, color: c } : undefined}
              >
                {l}
              </button>
            );
          })}
        </div>
        <button
          onClick={() => setLive(x => !x)}
          className={`${styles.liveBtn} ${live ? styles.liveBtnActive : ''}`}
        >
          <span className={`${styles.liveDot} ${live ? styles.liveDotActive : ''}`} />
          {live ? 'LIVE' : 'PAUSED'}
        </button>
        <button
          className={styles.clearBtn}
          onClick={async () => {
            if (!window.confirm('Delete all log files? This cannot be undone.')) return;
            try {
              const res = await dashFetch('/api/logs', { method: 'DELETE' });
              if (!res.ok) throw new Error(await res.text());
              setLogs([]);
            } catch (err) {
              alert('Failed to clear logs: ' + err.message);
            }
          }}
        >
          Clear
        </button>
      </div>

      <div ref={scrollRef} className={styles.stream}>
        {loading ? (
          <div className={styles.skeletonWrap}>
            {[52, 52, 52, 52, 52, 52, 52, 52].map((w, i) => (
              <div key={i} className={styles.skeletonRow}>
                <div className={styles.skeletonBar} style={{ width: w, animation: `shimmer 1.8s ease ${i * 0.1}s infinite` }} />
                <div className={styles.skeletonBar} style={{ width: 36, animation: `shimmer 1.8s ease ${i * 0.1 + 0.05}s infinite` }} />
                <div className={styles.skeletonBarFlex} style={{ animation: `shimmer 1.8s ease ${i * 0.1 + 0.1}s infinite` }} />
              </div>
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <div className={styles.empty}>No log entries yet.</div>
        ) : (
          filtered.map((l, i) => <LogRow key={i} l={l} />)
        )}
      </div>
    </div>
  );
}

function KindBadge({ kind }) {
  const c = KIND_COLORS[kind] || '#9ba39c';
  return (
    <span className={styles.kindBadge} style={{ color: c, background: c + '22', border: `1px solid ${c}55` }}>
      {kind}
    </span>
  );
}

function LogRow({ l }) {
  return (
    <div className={styles.row}>
      <span className={styles.rowTime}>{l.t}</span>
      <KindBadge kind={l.kind} />
      <LogBody l={l} />
    </div>
  );
}

function LogBody({ l }) {
  if (l.type === 'http' && l.parsed) {
    const { method, path, status, ms } = l.parsed;
    return (
      <span className={styles.bodyHttp}>
        <span className={styles.bodyMethod} style={{ color: METHOD_COLORS[method] || '#9ba39c' }}>{method}</span>
        <span className={styles.bodyStatus} style={{ color: statusColor(status) }}>{status}</span>
        <span className={styles.bodyPath}>{path}</span>
        <span className={styles.bodyDim}>{ms}ms</span>
        {l.tunnel ? <span className={styles.bodyDim}>· {l.tunnel}</span> : null}
        {l.remote ? <span className={styles.bodyDim}>← {l.remote}</span> : null}
        {l.bytes  ? <span className={styles.bodyDim}>· {formatBytes(l.bytes)}</span> : null}
      </span>
    );
  }

  if (l.type === 'event') {
    return (
      <span className={styles.bodyEvent}>
        <span className={styles.bodyAction}>{l.action || '—'}</span>
        {l.tunnel ? <span className={styles.bodyDim}>· {l.tunnel}</span> : null}
        {l.remote ? <span className={styles.bodyDim}>← {l.remote}</span> : null}
        {l.bytes  ? <span className={styles.bodyDim}>· {formatBytes(l.bytes)}</span> : null}
      </span>
    );
  }

  const textColor =
    l.kind === 'ERROR' ? '#ff4d4d' :
    l.kind === 'WARN'  ? '#f5c542' :
    l.kind === 'OK'    ? '#00e87a' :
    l.kind === 'DEBUG' ? '#9b8fff' :
    'var(--text)';
  return (
    <span className={styles.bodySys} style={{ color: textColor }}>{l.msg || '—'}</span>
  );
}

function formatBytes(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
