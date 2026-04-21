function SettingsView({ config, loading }) {
  const [authToken, setAuthToken] = useState(config?.token || '');
  const [server, setServer] = useState(config?.server || '');
  const [showToken, setShowToken] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);

  function min(a,b) { return a < b ? a : b; }

  async function applyRestart() {
    setConfirmRestart(false);
    setRestarting(true);
    try {
      const res = await fetch('/api/restart', { method: 'POST' });
      if(!res.ok) throw new Error(await res.text());
    } catch(err) {
      alert("Failed to restart daemon: " + err.message);
    }
    setRestarting(false);
  }

  if (loading) {
    const skel = (w) => <div style={{ height: 32, width: w, background: 'var(--border2)', borderRadius: 2, animation: 'shimmer 1.6s ease infinite' }} />;
    return (
      <div style={{ flex: 1, overflowY: 'auto', padding: 32 }}>
        <div style={{ maxWidth: 600, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <div style={{ height: 16, width: 80, background: 'var(--border2)', borderRadius: 2, animation: 'shimmer 1.6s ease infinite' }} />
          {[1,2,3,4].map(i => (
            <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={{ height: 10, width: 100, background: 'var(--border2)', borderRadius: 2, animation: `shimmer 1.6s ease ${i*0.1}s infinite` }} />
              {skel('100%')}
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <>
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
            <Field
              label="Restart Daemon"
              sub="Applies any config changes added/removed."
              controlsStyle={{ flex: 1, minWidth: 0, display: 'flex' }}
            >
              <button onClick={() => setConfirmRestart(true)} disabled={restarting}
                style={{ float: 'right', marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 6, padding: '9px 20px', background: restarting ? 'var(--accent-dim)' : 'var(--accent)', border: 'none', color: restarting ? 'var(--accent)' : '#000', fontSize: 13, fontWeight: 600, cursor: 'pointer', letterSpacing: '.02em', transition: 'all .2s' }}>
                {restarting ? 'Restarting...' : 'Restart Now'}
              </button>
            </Field>
          </Section>
        </div>
      </div>

      {confirmRestart && (
        <div style={{ position: 'fixed', inset: 0, background: '#00000088', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100 }}
          onClick={() => setConfirmRestart(false)}>
          <div style={{ background: 'var(--panel)', border: '1px solid var(--border2)', width: 360, padding: 24 }} onClick={e => e.stopPropagation()}>
            <div style={{ fontSize: 15, fontWeight: 600, color: '#fff', marginBottom: 10 }}>Restart Daemon</div>
            <div style={{ fontSize: 13, color: 'var(--text-dim)', marginBottom: 20 }}>This will reconnect all active tunnels. Any in-flight connections will be dropped.</div>
            <div style={{ display: 'flex', gap: 10 }}>
              <button onClick={() => setConfirmRestart(false)}
                style={{ flex: 1, background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>
                Cancel
              </button>
              <button onClick={applyRestart}
                style={{ flex: 1, background: 'var(--accent)', border: 'none', color: '#000', padding: '8px', fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                Restart
              </button>
            </div>
          </div>
        </div>
      )}
    </>
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
function Field({ label, sub, children, controlsStyle }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
      <div style={{flex:1}}>
        <div style={{ fontSize: 13, color: 'var(--text)', fontWeight: 500 }}>{label}</div>
        {sub && <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 2 }}>{sub}</div>}
      </div>
      <div style={{ flexShrink: 0, minWidth: 200, ...controlsStyle }}>{children}</div>
    </div>
  );
}
