function TunnelDetail({ tunnel, onClose }) {
  if (!tunnel) return null;
  return (
    <div style={{ position: 'absolute', right: 0, top: 0, bottom: 0, width: 360, background: 'var(--panel)', borderLeft: '1px solid var(--border2)', display: 'flex', flexDirection: 'column', zIndex: 50, animation: 'slideIn .18s ease' }}>
      <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 10 }}>
        <StatusDot status={tunnel.status} />
        <span style={{ flex: 1, fontSize: 14, fontWeight: 600, color: '#fff' }}>Local Target: {tunnel.localPort}</span>
        <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-dim)' }}><Icon d={Icons.x} size={16} color="currentColor" /></button>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
        {[
          ['ID', tunnel.id],
          ['Public Endpoint', `${tunnel.proto}://${tunnel.publicUrl}`],
          ['Protocol', tunnel.proto.toUpperCase()],
          ['Status', tunnel.status],
          ['Requests', tunnel.requests.toLocaleString()],
          ['Bandwidth', tunnel.bandwidth],
        ].map(([k,v]) => (
          <div key={k} style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)', marginBottom: 3 }}>{k}</div>
            <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text)', wordBreak: 'break-all' }}>{v}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function StatsBar({ tunnels, server }) {
  const online = tunnels.length;
  const totalReqs = tunnels.reduce((a, t) => a + t.requests, 0);
  return (
    <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid var(--border)', flexShrink: 0, background: 'var(--panel)' }}>
      {[
        { label: 'Active Tunnels', value: `${online} connected`, accent: true },
        { label: 'Total Mocks', value: totalReqs.toLocaleString() },
        { label: 'Agent', value: 'v2.4.2' },
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

function App() {
  const [activeNav, setActiveNav] = useState('tunnels');
  const [tunnels, setTunnels] = useState([]);
  const [rawConfig, setRawConfig] = useState(null);
  const [selectedTunnel, setSelectedTunnel] = useState(null);
  const [initError, setInitError] = useState(null);
  
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
      
      const parsedTunnels = (cfg.forwards || []).map(f => {
        let pubUrl = '';
        if (f.protocol === 'http') pubUrl = f.domain || '(auto domain)';
        else pubUrl = f.remote_port > 0 ? `Port ${f.remote_port}` : '(auto port)';
        
        return {
          id: f.id,
          name: f.id,
          proto: f.protocol,
          localPort: f.local_addr,
          publicUrl: pubUrl,
          status: f.disabled ? 'offline' : 'online', 
          disabled: f.disabled,
          domain: f.domain,
          remotePort: f.remote_port,
          region: 'auto',
          requests: Math.floor(Math.random() * 500), 
          bandwidth: (Math.random() * 10).toFixed(1) + ' MB', 
          tags: [f.protocol]
        };
      });
      setTunnels(parsedTunnels);
      
    } catch(err) {
      console.error("Config fetch error", err);
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
    <div style={{ height: '100vh', display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
      <StatsBar tunnels={tunnels} server={rawConfig?.server} />
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden', position: 'relative' }}>
        <Sidebar active={activeNav} setActive={v => { setActiveNav(v); setSelectedTunnel(null); }} />
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
          {activeNav === 'tunnels' && <TunnelsView tunnels={tunnels} reloadConfig={loadConfig} onSelectTunnel={t => setSelectedTunnel(t)} />}
          {activeNav === 'logs' && <LogsView />}
          {activeNav === 'settings' && <SettingsView config={rawConfig} />}
          {selectedTunnel && activeNav === 'tunnels' && <TunnelDetail tunnel={selectedTunnel} onClose={() => setSelectedTunnel(null)} />}
        </div>
      </div>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App />);
