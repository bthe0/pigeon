import { useState, useEffect, useRef } from 'react';

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

// ── Classification ───────────────────────────────────────────────────────────
// Normalise each API log row into a uniform { time, kind, msg } shape so
// the render pass doesn't have to branch on record type.

const DAEMON_RULES = [
  // ERROR — things that genuinely went wrong.
  { kind: 'ERROR', match: /\berror\b|\bfail(ed)?\b|\brefus(ed|al)\b|dial local|cannot |panic/i },
  // WARN — degraded but not broken.
  { kind: 'WARN',  match: /\bwarn(ing)?\b|disconnect|reconnect|retry|attempt|rate[- ]limit|rejected|locked/i },
  // OK — positive lifecycle events.
  { kind: 'OK',    match: /\bconnected\b|forward ready|dashboard at|web ui|listening on|tunnel ready|enabled|started/i },
  // DEBUG — verbose operational noise.
  { kind: 'DEBUG', match: /attempt \d|\bping\b|\bpong\b|reloaded|skipping|saving/i },
];

function classifyDaemon(msg) {
  for (const r of DAEMON_RULES) if (r.match.test(msg)) return r.kind;
  return 'INFO';
}

// Turn an HTTP traffic action like "GET /foo 200 12ms" into its parts so we
// can render each piece with its own colour.
function parseHTTPAction(action) {
  const m = action && action.match(/^(\w+)\s+(\S+)\s+(\d{3})\s+(\d+)ms$/);
  if (!m) return null;
  return { method: m[1], path: m[2], status: parseInt(m[3], 10), ms: parseInt(m[4], 10) };
}

function classifyRow(r) {
  const d = new Date(r.time);
  const t = `${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}:${String(d.getSeconds()).padStart(2,'0')}`;

  // Backend mixes cases: daemon.log tail is stamped "DAEMON" uppercase,
  // client-written NDJSON uses lowercase "http"/"tcp"/"udp". Normalise once
  // so downstream branches are case-insensitive.
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
    // Non-parseable HTTP actions (e.g. "CONNECT" when the client opens a
    // new yamux stream) still need to render; tag them as a generic event.
    return { t, kind: 'HTTP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  if (proto === 'TCP') {
    return { t, kind: 'TCP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  if (proto === 'UDP') {
    return { t, kind: 'UDP', type: 'event', action, tunnel, remote, bytes, proto };
  }
  // Unknown/missing protocol but we still have useful per-request fields
  // (historic NDJSON entries predate the protocol column). Render whatever
  // we can instead of showing "?".
  if (tunnel || remote || bytes) {
    return { t, kind: 'INFO', type: 'event', action: action || 'request', tunnel, remote, bytes, proto: proto || '' };
  }
  // Truly nothing to show — fall back to a plain system message.
  return { t, kind: 'INFO', type: 'sys', msg: `${r.protocol || '?'} ${action}`.trim() };
}

// ── Component ────────────────────────────────────────────────────────────────

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
        // 'HTTP' tab surfaces every http-related row (parsed access log OR
        // stream-open events that get kind=HTTP via classifyRow).
        if (kindFilter === 'HTTP' && (l.type === 'http' || (l.type === 'event' && (l.proto === 'HTTP' || l.proto === 'HTTPS')))) return true;
        if (kindFilter === 'TCP' && l.type === 'event' && l.proto === 'TCP') return true;
        if (kindFilter === 'UDP' && l.type === 'event' && l.proto === 'UDP') return true;
        return false;
      });

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div className="logs-toolbar" style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: '#fff' }}>System Logs</div>
          <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>{filtered.length} entries</div>
        </div>
        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
          {filters.map(l => {
            const active = kindFilter === l;
            const c = KIND_COLORS[l] || 'var(--accent)';
            return (
              <button key={l} onClick={() => setKindFilter(l)}
                style={{
                  padding: '4px 8px',
                  background: active ? c + '22' : 'var(--panel2)',
                  border: `1px solid ${active ? c : 'var(--border2)'}`,
                  color: active ? c : 'var(--text-dim)',
                  fontSize: 10, fontFamily: 'var(--mono)', fontWeight: 600,
                  cursor: 'pointer', letterSpacing: '.06em',
                }}>{l}</button>
            );
          })}
        </div>
        <button onClick={() => setLive(x=>!x)}
          style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 10px', background: live ? 'var(--accent-dim)' : 'var(--panel2)', border: `1px solid ${live ? 'var(--accent-mid)' : 'var(--border2)'}`, color: live ? 'var(--accent)' : 'var(--text-dim)', fontSize: 11, fontWeight: 600, cursor: 'pointer', letterSpacing: '.04em' }}>
          <span style={{ width: 6, height: 6, borderRadius: '50%', background: live ? 'var(--accent)' : 'var(--text-dim)', display: 'inline-block', animation: live ? 'pulse 1.5s ease infinite' : 'none' }} />
          {live ? 'LIVE' : 'PAUSED'}
        </button>
        <button onClick={async () => {
            if (!window.confirm('Delete all log files? This cannot be undone.')) return;
            try {
              const res = await dashFetch('/api/logs', { method: 'DELETE' });
              if (!res.ok) throw new Error(await res.text());
              setLogs([]);
            } catch (err) {
              alert('Failed to clear logs: ' + err.message);
            }
          }}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '5px 10px', color: 'var(--text-dim)', fontSize: 11, cursor: 'pointer', fontFamily: 'var(--sans)' }}>Clear</button>
      </div>

      <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: '8px 24px', fontFamily: 'var(--mono)', fontSize: 11.5, lineHeight: 1.8 }}>
        {loading ? (
          <div style={{ paddingTop: 16 }}>
            {[52, 52, 52, 52, 52, 52, 52, 52].map((w, i) => (
              <div key={i} style={{ display: 'flex', gap: 14, padding: '6px 0', borderBottom: '1px solid var(--border)', alignItems: 'center' }}>
                <div style={{ height: 7, width: w,  background: 'var(--border2)', borderRadius: 2, flexShrink: 0, animation: `shimmer 1.8s ease ${i * 0.1}s infinite` }} />
                <div style={{ height: 7, width: 36, background: 'var(--border2)', borderRadius: 2, flexShrink: 0, animation: `shimmer 1.8s ease ${i * 0.1 + 0.05}s infinite` }} />
                <div style={{ height: 7, flex: 1,   background: 'var(--border2)', borderRadius: 2, animation: `shimmer 1.8s ease ${i * 0.1 + 0.1}s infinite` }} />
              </div>
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <div style={{ color: 'var(--text-dim)', textAlign: 'center', marginTop: 40, fontFamily: 'var(--sans)' }}>No log entries yet.</div>
        ) : (
          filtered.map((l, i) => <LogRow key={i} l={l} />)
        )}
      </div>
    </div>
  );
}

// ── Log row ──────────────────────────────────────────────────────────────────

function KindBadge({ kind }) {
  const c = KIND_COLORS[kind] || '#9ba39c';
  return (
    <span style={{
      flexShrink: 0, display: 'inline-block', width: 46, textAlign: 'center',
      fontWeight: 700, fontSize: 10, letterSpacing: '.06em',
      color: c, background: c + '22', border: `1px solid ${c}55`,
      padding: '0 4px',
    }}>{kind}</span>
  );
}

function LogRow({ l }) {
  return (
    <div style={{ display: 'flex', gap: 12, padding: '3px 0', alignItems: 'baseline' }}>
      <span style={{ color: 'var(--text-dim)', flexShrink: 0, userSelect: 'none', fontSize: 10 }}>{l.t}</span>
      <KindBadge kind={l.kind} />
      <LogBody l={l} />
    </div>
  );
}

function LogBody({ l }) {
  // Fully parsed HTTP access-log line: show each piece in its own colour.
  if (l.type === 'http' && l.parsed) {
    const { method, path, status, ms } = l.parsed;
    return (
      <span style={{ flex: 1, minWidth: 0, display: 'flex', gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
        <span style={{ color: METHOD_COLORS[method] || '#9ba39c', fontWeight: 700 }}>{method}</span>
        <span style={{ color: statusColor(status), fontWeight: 700 }}>{status}</span>
        <span style={{ color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{path}</span>
        <span style={{ color: 'var(--text-dim)' }}>{ms}ms</span>
        {l.tunnel ? <span style={{ color: 'var(--text-dim)' }}>· {l.tunnel}</span> : null}
        {l.remote ? <span style={{ color: 'var(--text-dim)' }}>← {l.remote}</span> : null}
        {l.bytes  ? <span style={{ color: 'var(--text-dim)' }}>· {formatBytes(l.bytes)}</span> : null}
      </span>
    );
  }

  // Protocol events (CONNECT, IN, OUT) that don't fit the access-log shape.
  if (l.type === 'event') {
    return (
      <span style={{ flex: 1, minWidth: 0, display: 'flex', gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
        <span style={{ color: 'var(--text)', fontWeight: 600 }}>{l.action || '—'}</span>
        {l.tunnel ? <span style={{ color: 'var(--text-dim)' }}>· {l.tunnel}</span> : null}
        {l.remote ? <span style={{ color: 'var(--text-dim)' }}>← {l.remote}</span> : null}
        {l.bytes  ? <span style={{ color: 'var(--text-dim)' }}>· {formatBytes(l.bytes)}</span> : null}
      </span>
    );
  }

  // System / daemon message — tint the text for WARN/ERROR so the stream
  // stays readable even if you've glossed over the badge.
  const textColor =
    l.kind === 'ERROR' ? '#ff4d4d' :
    l.kind === 'WARN'  ? '#f5c542' :
    l.kind === 'OK'    ? '#00e87a' :
    l.kind === 'DEBUG' ? '#9b8fff' :
    'var(--text)';
  return (
    <span style={{ flex: 1, minWidth: 0, color: textColor, overflowWrap: 'anywhere' }}>{l.msg || '—'}</span>
  );
}

function formatBytes(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
