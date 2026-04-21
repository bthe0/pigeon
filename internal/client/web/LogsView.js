function LogsView() {
  const [logs, setLogs] = useState([]);
  const [live, setLive] = useState(true);
  const [levelFilter, setLevelFilter] = useState('ALL');
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
        const res = await fetch('/api/logs');
        if (res.ok) {
          const raw = await res.json() || [];
          const formatted = raw.map(r => {
             const d = new Date(r.time);
             const t = `${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}:${String(d.getSeconds()).padStart(2,'0')}`;

             let level = 'INFO';
             let msg = r.action;
             if (r.protocol === 'DAEMON') {
               const m = msg.toLowerCase();
               if (m.includes('error') || m.includes('fail') || m.includes('dial local') ||
                   m.includes('server error') || m.includes('client init') ||
                   m.includes('save config') || m.includes('udp session') || m.includes('udp write')) {
                 level = 'ERROR';
               } else if (m.includes('disconnect') || m.includes('reconnect') ||
                          m.includes('unknown forward') || m.includes('retry') || m.includes('attempt')) {
                 level = 'WARN';
               }
             } else {
               msg = `[${r.forward_id}] ${r.protocol} ${r.action}`;
               if (r.remote_addr) msg += ` from ${r.remote_addr}`;
               if (r.bytes) msg += ` — ${r.bytes} bytes`;
             }

             return { t, level, msg };
          });
          setLogs(formatted);
          setLoading(false);
        }
      } catch (err) { setLoading(false); }
    };
    fetchLogs();
    const iv = setInterval(fetchLogs, 1000);
    return () => clearInterval(iv);
  }, [live]);

  useEffect(() => {
    if (loading || !stickToBottomRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    requestAnimationFrame(() => {
      el.scrollTop = el.scrollHeight;
    });
  }, [loading, logs, levelFilter]);

  const LEVEL_COLORS = { INFO: 'var(--text-mid)', WARN: 'var(--yellow)', ERROR: 'var(--red)', DEBUG: '#9b8fff' };
  const filtered = levelFilter === 'ALL' ? logs : logs.filter(l => l.level === levelFilter);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div className="logs-toolbar" style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: '#fff' }}>System Logs</div>
          <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>{filtered.length} entries</div>
        </div>
        <div style={{ display: 'flex', gap: 4 }}>
          {['ALL','INFO','WARN','ERROR'].map(l => (
            <button key={l} onClick={() => setLevelFilter(l)}
              style={{ padding: '4px 8px', background: levelFilter===l ? (LEVEL_COLORS[l]||'var(--accent)')+'22' : 'var(--panel2)', border: `1px solid ${levelFilter===l ? (LEVEL_COLORS[l]||'var(--accent)') : 'var(--border2)'}`, color: levelFilter===l ? (LEVEL_COLORS[l]||'var(--accent)') : 'var(--text-dim)', fontSize: 10, fontFamily: 'var(--mono)', fontWeight: 600, cursor: 'pointer', letterSpacing: '.06em' }}>
              {l}
            </button>
          ))}
        </div>
        <button onClick={() => setLive(x=>!x)}
          style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 10px', background: live ? 'var(--accent-dim)' : 'var(--panel2)', border: `1px solid ${live ? 'var(--accent-mid)' : 'var(--border2)'}`, color: live ? 'var(--accent)' : 'var(--text-dim)', fontSize: 11, fontWeight: 600, cursor: 'pointer', letterSpacing: '.04em' }}>
          <span style={{ width: 6, height: 6, borderRadius: '50%', background: live ? 'var(--accent)' : 'var(--text-dim)', display: 'inline-block', animation: live ? 'pulse 1.5s ease infinite' : 'none' }} />
          {live ? 'LIVE' : 'PAUSED'}
        </button>
        <button onClick={() => setLogs([])} style={{ background: 'none', border: '1px solid var(--border2)', padding: '5px 10px', color: 'var(--text-dim)', fontSize: 11, cursor: 'pointer', fontFamily: 'var(--sans)' }}>Clear</button>
      </div>
      <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: '8px 24px', fontFamily: 'var(--mono)', fontSize: 11.5, lineHeight: 1.8 }}>
        {loading ? (
          <div style={{ paddingTop: 16 }}>
            {[
              { time: 52, level: 32, msg: '68%' },
              { time: 52, level: 32, msg: '42%' },
              { time: 52, level: 32, msg: '81%' },
              { time: 52, level: 32, msg: '35%' },
              { time: 52, level: 32, msg: '57%' },
              { time: 52, level: 32, msg: '73%' },
              { time: 52, level: 32, msg: '29%' },
              { time: 52, level: 32, msg: '61%' },
            ].map((row, i) => (
              <div key={i} style={{ display: 'flex', gap: 14, padding: '6px 0', borderBottom: '1px solid var(--border)', alignItems: 'center' }}>
                <div style={{ height: 7, width: row.time, background: 'var(--border2)', borderRadius: 2, flexShrink: 0, animation: `shimmer 1.8s ease ${i * 0.1}s infinite` }} />
                <div style={{ height: 7, width: row.level, background: 'var(--border2)', borderRadius: 2, flexShrink: 0, animation: `shimmer 1.8s ease ${i * 0.1 + 0.05}s infinite` }} />
                <div style={{ height: 7, width: row.msg, background: 'var(--border2)', borderRadius: 2, animation: `shimmer 1.8s ease ${i * 0.1 + 0.1}s infinite` }} />
              </div>
            ))}
          </div>
        ) : (
          <>
            {filtered.map((l, i) => (
              <div key={i} style={{ display: 'flex', gap: 14, padding: '2px 0', borderBottom: '1px solid var(--border)10' }}>
                <span style={{ color: 'var(--text-dim)', flexShrink: 0, userSelect: 'none' }}>{l.t}</span>
                <span style={{ flexShrink: 0, fontWeight: 600, width: 40, color: LEVEL_COLORS[l.level] || 'var(--text-mid)' }}>{l.level}</span>
                <span style={{ color: l.level==='ERROR' ? 'var(--red)' : l.level==='WARN' ? 'var(--yellow)' : 'var(--text)' }}>{l.msg}</span>
              </div>
            ))}
            {filtered.length === 0 && <div style={{ color: 'var(--text-dim)', textAlign: 'center', marginTop: 40, fontFamily: 'var(--sans)' }}>No traffic logged yet.</div>}
          </>
        )}
      </div>
    </div>
  );
}
