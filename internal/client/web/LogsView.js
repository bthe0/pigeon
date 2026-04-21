function LogsView() {
  const [logs, setLogs] = useState([]);
  
  useEffect(() => {
    let intv = setInterval(async () => {
      try {
        const res = await fetch('/api/logs');
        if (res.ok) setLogs(await res.json() || []);
      } catch (err) {}
    }, 1000);
    return () => clearInterval(intv);
  }, []);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        <div style={{ fontSize: 15, fontWeight: 600, color: '#fff', letterSpacing: '-.01em' }}>Traffic Logs</div>
        <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>Real-time daemon events</div>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: 24, display: 'flex', flexDirection: 'column-reverse' }}>
        {logs.slice().reverse().map((l, i) => (
          <div key={i} style={{ fontFamily: 'var(--mono)', fontSize: 12, marginBottom: 8, display: 'flex', alignItems: 'center', gap: 12 }}>
            <span style={{ color: 'var(--text-dim)' }}>{new Date(l.time).toLocaleTimeString()}</span>
            <span style={{ color: PROTO_COLORS[l.protocol]||'var(--accent)', fontWeight: 600, width: 40 }}>{l.protocol}</span>
            <span style={{ color: '#fff', width: 90 }}>{l.forward_id}</span>
            <span style={{ color: 'var(--text-mid)', width: 140 }}>{l.remote_addr}</span>
            <span style={{ color: 'var(--accent)', minWidth: 60 }}>{l.action}</span>
            {l.bytes ? <span style={{ color: 'var(--text-dim)' }}>{l.bytes} bytes</span> : null}
          </div>
        ))}
        {logs.length === 0 && <div style={{ color: 'var(--text-dim)', textAlign: 'center', marginTop: 40 }}>No traffic logged yet.</div>}
      </div>
    </div>
  );
}
