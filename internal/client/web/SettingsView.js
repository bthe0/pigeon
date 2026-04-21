function SettingsView({ config }) {
  const [authToken, setAuthToken] = useState(config?.token || '');
  const [server, setServer] = useState(config?.server || '');
  const [showToken, setShowToken] = useState(false);
  const [restarting, setRestarting] = useState(false);

  function min(a,b) { return a < b ? a : b; }

  async function applyRestart() {
    if(!confirm("Restart Pigeon Daemon using current config? (Changes require a restart)")) return;
    setRestarting(true);
    try {
      const res = await fetch('/api/restart', { method: 'POST' });
      if(!res.ok) throw new Error(await res.text());
      alert("Daemon restarted successfully.");
    } catch(err) {
      alert("Failed to restart daemon: " + err.message);
    }
    setRestarting(false);
  }

  return (
    <div style={{ flex: 1, overflowY: 'auto', padding: 32 }}>
      <div style={{ maxWidth: 600 }}>
        <div style={{ fontSize: 15, fontWeight: 600, color: '#fff', marginBottom: 4 }}>Settings</div>
        <div style={{ fontSize: 12, color: 'var(--text-dim)', marginBottom: 28 }}>Agent configuration and authentication</div>

        <Section title="Authentication & Server">
          <Field label="Auth Token" sub="Used to authenticate your tunnel agent">
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input readOnly value={showToken ? authToken : '•'.repeat(Math.max(10, min(authToken?authToken.length:0, 20)))}
                style={{ flex: 1, background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '7px 10px', fontSize: 12, fontFamily: 'var(--mono)', outline: 'none' }} />
              <button onClick={() => setShowToken(x=>!x)} style={{ background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text-mid)', padding: '7px 10px', fontSize: 11, cursor: 'pointer', fontFamily: 'var(--sans)', whiteSpace: 'nowrap' }}>{showToken ? 'Hide' : 'Reveal'}</button>
            </div>
          </Field>
          
          <Field label="Server Address" sub="The upstream Pigeon server">
            <input readOnly value={server}
              style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '7px 10px', fontSize: 12, fontFamily: 'var(--mono)', outline: 'none' }} />
          </Field>
        </Section>
        
        <Section title="Actions">
          <Field label="Restart Daemon" sub="Applies any config changes added/removed.">
            <button onClick={applyRestart} disabled={restarting}
              style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '9px 20px', background: restarting ? 'var(--accent-dim)' : 'var(--accent)', border: 'none', color: restarting ? 'var(--accent)' : '#000', fontSize: 13, fontWeight: 600, cursor: 'pointer', letterSpacing: '.02em', transition: 'all .2s' }}>
              {restarting ? 'Restarting...' : 'Restart Now'}
            </button>
          </Field>
        </Section>
      </div>
    </div>
  );
}

function Section({ title, children }) {
  return (
    <div style={{ marginBottom: 28 }}>
      <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '.08em', textTransform: 'uppercase', color: 'var(--text-dim)', marginBottom: 12, paddingBottom: 8, borderBottom: '1px solid var(--border)' }}>{title}</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>{children}</div>
    </div>
  );
}
function Field({ label, sub, children }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
      <div style={{flex:1}}>
        <div style={{ fontSize: 13, color: 'var(--text)', fontWeight: 500 }}>{label}</div>
        {sub && <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 2 }}>{sub}</div>}
      </div>
      <div style={{ flexShrink: 0, minWidth: 200 }}>{children}</div>
    </div>
  );
}
