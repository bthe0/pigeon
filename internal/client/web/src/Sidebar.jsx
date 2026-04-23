import { Icon, Icons } from './Icons';
import styles from './Sidebar.module.css';

export function Sidebar({ active, setActive, onLogout }) {
  const nav = [
    { id: 'tunnels', icon: Icons.tunnel, label: 'Tunnels' },
    { id: 'inspector', icon: Icons.activity, label: 'Inspector' },
    { id: 'logs', icon: Icons.log, label: 'Logs' },
    { id: 'settings', icon: Icons.settings, label: 'Settings' },
  ];
  return (
    <div className={styles.sidebar}>
      <button onClick={() => setActive('tunnels')} className={styles.header} aria-label="Go to Tunnels home">
        <div className={styles.headerRow}>
          <img src="/logo.png" alt="Pigeon logo" className={styles.logo} />
          <span className={styles.brand}>pigeon</span>
        </div>
        <div className={styles.tagline}>tunnel agent connected</div>
      </button>

      <div className={styles.system}>
        <div className={styles.systemLabel}>System</div>
        <div className={styles.systemValue}>
          <Icon d={Icons.globe} size={11} color="var(--accent)" />
          Local Agent
        </div>
      </div>

      <nav className={styles.nav}>
        {nav.map(n => (
          <button
            key={n.id}
            onClick={() => setActive(n.id)}
            data-active={active === n.id}
            className={`${styles.navBtn} ${active === n.id ? styles.navBtnActive : ''}`}
          >
            <Icon d={n.icon} size={15} color="currentColor" />
            {n.label}
          </button>
        ))}
        <div className={styles.navSpacer} />
        <button onClick={onLogout} className={styles.logout}>
          <Icon d={Icons.x} size={15} color="currentColor" />
          Log Out
        </button>
      </nav>
    </div>
  );
}
