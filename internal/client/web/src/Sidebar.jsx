import React from 'react';
import { Icon, Icons } from './Icons';

export function Sidebar({ active, setActive, onLogout }) {
  const nav = [
    { id: 'tunnels', icon: Icons.tunnel, label: 'Tunnels' },
    { id: 'inspector', icon: Icons.activity, label: 'Inspector' },
    { id: 'logs', icon: Icons.log, label: 'Logs' },
    { id: 'settings', icon: Icons.settings, label: 'Settings' },
  ];
  return (
    <div className="sidebar" style={{ width: 200, background: 'var(--panel)', borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
      {/* Logo */}
      <div className="sidebar-header" style={{ padding: '20px 16px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <img src="/logo.png" alt="Pigeon logo" style={{ width: 28, height: 28, objectFit: 'contain', flexShrink: 0 }} />
          <span style={{ fontFamily: 'var(--mono)', fontWeight: 600, fontSize: 14, letterSpacing: '.06em', color: '#fff' }}>pigeon</span>
        </div>
        <div style={{ marginTop: 4, fontFamily: 'var(--mono)', fontSize: 10, color: 'var(--text-dim)' }}>tunnel agent connected</div>
      </div>

      <div className="sidebar-system" style={{ padding: '10px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ fontSize: 10, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.06em', textTransform: 'uppercase', fontWeight: 500 }}>System</div>
        <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--accent)', display: 'flex', alignItems: 'center', gap: 4 }}>
          <Icon d={Icons.globe} size={11} color="var(--accent)" />
          Local Agent
        </div>
      </div>

      <nav style={{ flex: 1, padding: '8px 0', display: 'flex', flexDirection: 'column' }}>
        {nav.map(n => (
          <button key={n.id} onClick={() => setActive(n.id)} className="sidebar-nav-btn" data-active={active === n.id}
            style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, padding: '9px 16px', background: active === n.id ? 'var(--accent-dim)' : 'none', border: 'none', borderLeft: `2px solid ${active === n.id ? 'var(--accent)' : 'transparent'}`, cursor: 'pointer', color: active === n.id ? 'var(--accent)' : 'var(--text-dim)', fontSize: 13, fontFamily: 'var(--sans)', fontWeight: active === n.id ? 500 : 400, textAlign: 'left', transition: 'all .12s' }}>
            <Icon d={n.icon} size={15} color="currentColor" />
            {n.label}
          </button>
        ))}
        <div style={{ flex: 1 }} />
        <button onClick={onLogout} className="sidebar-nav-btn"
          style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, padding: '12px 16px', background: 'none', border: 'none', borderTop: '1px solid var(--border)', cursor: 'pointer', color: 'var(--red)', fontSize: 13, fontFamily: 'var(--sans)', fontWeight: 400, textAlign: 'left', transition: 'all .12s' }}>
          <Icon d={Icons.x} size={15} color="currentColor" />
          Log Out
        </button>
      </nav>
    </div>
  );
}

