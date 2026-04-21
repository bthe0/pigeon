function Sidebar({ active, setActive }) {
  const nav = [
    { id: 'tunnels', icon: Icons.tunnel, label: 'Tunnels' },
    { id: 'inspector', icon: Icons.activity, label: 'Inspector' },
    { id: 'logs', icon: Icons.log, label: 'Logs' },
    { id: 'settings', icon: Icons.settings, label: 'Settings' },
  ];
  return (
    <div style={{ width: 200, background: 'var(--panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      {/* Logo */}
      <div style={{ padding: '20px 16px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div style={{ width: 24, height: 24, background: 'var(--accent)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Icon d={Icons.zap} size={14} color="#000" />
          </div>
          <span style={{ fontFamily: 'var(--mono)', fontWeight: 600, fontSize: 14, letterSpacing: '.06em', color: '#fff' }}>pigeon</span>
        </div>
        <div style={{ marginTop: 4, fontFamily: 'var(--mono)', fontSize: 10, color: 'var(--text-dim)' }}>tunnel agent connected</div>
      </div>

      <div style={{ padding: '10px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ fontSize: 10, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.06em', textTransform: 'uppercase', fontWeight: 500 }}>System</div>
        <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--accent)', display: 'flex', alignItems: 'center', gap: 4 }}>
          <Icon d={Icons.globe} size={11} color="var(--accent)" />
          Local Agent
        </div>
      </div>

      <nav style={{ flex: 1, padding: '8px 0' }}>
        {nav.map(n => (
          <button key={n.id} onClick={() => setActive(n.id)}
            style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, padding: '9px 16px', background: active === n.id ? 'var(--accent-dim)' : 'none', border: 'none', borderLeft: `2px solid ${active === n.id ? 'var(--accent)' : 'transparent'}`, cursor: 'pointer', color: active === n.id ? 'var(--accent)' : 'var(--text-dim)', fontSize: 13, fontFamily: 'var(--sans)', fontWeight: active === n.id ? 500 : 400, textAlign: 'left', transition: 'all .12s' }}>
            <Icon d={n.icon} size={15} color="currentColor" />
            {n.label}
          </button>
        ))}
      </nav>
    </div>
  );
}
