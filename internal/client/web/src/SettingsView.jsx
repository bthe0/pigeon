import { useEffect, useRef, useState } from 'react';
import styles from './SettingsView.module.css';

export function SettingsView({ config, loading, dashFetch }) {
  const authToken = config?.token || '';
  const server = config?.server || '';
  const [showToken, setShowToken] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);

  const [validating, setValidating] = useState(false);
  const [validateResult, setValidateResult] = useState(null);

  const [daemonPaused, setDaemonPaused] = useState(false);
  const [daemonBusy, setDaemonBusy] = useState(false);

  const [importing, setImporting] = useState(false);
  const [confirmImport, setConfirmImport] = useState(null);
  const fileInputRef = useRef(null);

  function min(a, b) { return a < b ? a : b; }

  useEffect(() => {
    if (loading) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await dashFetch('/api/daemon/state');
        if (!res.ok) return;
        const j = await res.json();
        if (!cancelled) setDaemonPaused(!!j.paused);
      } catch {}
    })();
    return () => { cancelled = true; };
  }, [loading]);

  async function applyRestart() {
    setConfirmRestart(false);
    setRestarting(true);
    try {
      const res = await dashFetch('/api/restart', { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
    } catch (err) {
      alert("Failed to restart daemon: " + err.message);
    }
    setRestarting(false);
  }

  async function validateToken() {
    setValidating(true);
    setValidateResult(null);
    try {
      const res = await dashFetch('/api/token/validate', { method: 'POST' });
      if (!res.ok) {
        setValidateResult({ ok: false, error: `HTTP ${res.status}` });
      } else {
        const j = await res.json();
        setValidateResult(j);
      }
    } catch (err) {
      setValidateResult({ ok: false, error: err.message });
    }
    setValidating(false);
  }

  async function toggleDaemon() {
    setDaemonBusy(true);
    const target = daemonPaused ? 'start' : 'stop';
    try {
      const res = await dashFetch('/api/daemon/' + target, { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
      setDaemonPaused(!daemonPaused);
    } catch (err) {
      alert('Failed to ' + target + ' daemon: ' + err.message);
    }
    setDaemonBusy(false);
  }

  async function exportConfig() {
    try {
      const res = await dashFetch('/api/config/export');
      if (!res.ok) throw new Error(await res.text());
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'pigeon-config.json';
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      alert('Export failed: ' + err.message);
    }
  }

  function pickImportFile() {
    fileInputRef.current?.click();
  }

  function onFileChosen(e) {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f) return;
    setConfirmImport(f);
  }

  async function applyImport() {
    const f = confirmImport;
    setConfirmImport(null);
    if (!f) return;
    setImporting(true);
    try {
      const text = await f.text();
      JSON.parse(text);
      const res = await dashFetch('/api/config/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: text,
      });
      if (!res.ok) throw new Error(await res.text());
      alert('Config imported. Reloading dashboard…');
      window.location.reload();
    } catch (err) {
      alert('Import failed: ' + err.message);
    }
    setImporting(false);
  }

  if (loading) {
    return (
      <div className={styles.skeletonPage}>
        <div className={styles.skeletonPanel}>
          <div className={styles.skeletonLabel} />
          {[1, 2, 3, 4].map(i => (
            <div key={i} className={styles.skeletonField}>
              <div className={styles.skeletonSubLabel} style={{ animationDelay: `${i * 0.1}s` }} />
              <div className={styles.skeletonBlock} />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <>
      <div className={styles.page}>
        <div className={styles.panel}>
          <div className={styles.heading}>Settings</div>
          <div className={styles.subheading}>Agent configuration and authentication</div>

          <Section title="Authentication & Server">
            <Field label="Auth Token" sub="Used to authenticate your tunnel agent">
              <div className={styles.tokenRow}>
                <input
                  readOnly
                  className={styles.tokenInput}
                  value={showToken ? authToken : '•'.repeat(Math.max(10, min(authToken ? authToken.length : 0, 20)))}
                />
                <button className={styles.tokenBtn} onClick={() => setShowToken(x => !x)}>
                  {showToken ? 'Hide' : 'Reveal'}
                </button>
              </div>
            </Field>

            <Field label="Server Address" sub="The upstream Pigeon server">
              <input readOnly className={styles.readonlyInput} value={server} />
            </Field>

            <div className={styles.validateRow}>
              <button className={styles.secondaryBtn} onClick={validateToken} disabled={validating}>
                {validating ? 'Validating…' : 'Validate Token with Server'}
              </button>
              {validateResult && (
                <span className={`${styles.validateResult} ${validateResult.ok ? styles.validateOk : styles.validateErr}`}>
                  {validateResult.ok ? '✓ Token accepted' : '✗ ' + (validateResult.error || 'Invalid')}
                </span>
              )}
            </div>
          </Section>

          <Section title="Daemon">
            <Field
              label={daemonPaused ? 'Tunneling stopped' : 'Tunneling running'}
              sub={daemonPaused
                ? 'All tunnels are disconnected. Start the daemon to reconnect.'
                : 'The daemon is actively keeping your tunnels connected.'}
              controlsClassName={styles.fieldControlsRow}
            >
              <div className={styles.daemonControls}>
                <span className={`${styles.statusDot} ${daemonPaused ? styles.statusDotPaused : styles.statusDotRunning}`} />
                <button
                  onClick={toggleDaemon}
                  disabled={daemonBusy}
                  className={daemonPaused ? styles.primaryBtn : styles.ghostBtn}
                >
                  {daemonBusy ? '…' : (daemonPaused ? 'Start Daemon' : 'Stop Daemon')}
                </button>
              </div>
            </Field>
          </Section>

          <Section title="Configuration">
            <Field
              label="Export Config"
              sub="Download the agent configuration as JSON."
              controlsClassName={styles.fieldControlsRow}
            >
              <button className={`${styles.secondaryBtn} ${styles.alignRight}`} onClick={exportConfig}>
                Download JSON
              </button>
            </Field>

            <Field
              label="Import Config"
              sub="Replace the current configuration with a JSON file."
              controlsClassName={styles.fieldControlsRow}
            >
              <button
                className={`${styles.secondaryBtn} ${styles.alignRight}`}
                onClick={pickImportFile}
                disabled={importing}
              >
                {importing ? 'Importing…' : 'Choose File…'}
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept="application/json,.json"
                onChange={onFileChosen}
                className={styles.hiddenFile}
              />
            </Field>
          </Section>

          <Section title="Actions">
            <Field
              label="Restart Daemon"
              sub="Applies any config changes added/removed."
              controlsClassName={styles.fieldControlsRow}
            >
              <button className={styles.restartBtn} onClick={() => setConfirmRestart(true)} disabled={restarting}>
                {restarting ? 'Restarting...' : 'Restart Now'}
              </button>
            </Field>
          </Section>
        </div>
      </div>

      {confirmRestart && (
        <ConfirmModal
          title="Restart Daemon"
          message="This will reconnect all active tunnels. Any in-flight connections will be dropped."
          confirmLabel="Restart"
          onCancel={() => setConfirmRestart(false)}
          onConfirm={applyRestart}
        />
      )}

      {confirmImport && (
        <ConfirmModal
          title="Import Config"
          message={`Replace the current configuration with ${confirmImport.name}? This overwrites your token, server, and all forwards.`}
          confirmLabel="Import"
          onCancel={() => setConfirmImport(null)}
          onConfirm={applyImport}
        />
      )}
    </>
  );
}

function ConfirmModal({ title, message, confirmLabel, onCancel, onConfirm }) {
  return (
    <div className={styles.modalBackdrop} onClick={onCancel}>
      <div className={styles.modalCard} onClick={e => e.stopPropagation()}>
        <div className={styles.modalTitle}>{title}</div>
        <div className={styles.modalBody}>{message}</div>
        <div className={styles.modalButtons}>
          <button className={styles.modalCancel} onClick={onCancel}>Cancel</button>
          <button className={styles.modalConfirm} onClick={onConfirm}>{confirmLabel}</button>
        </div>
      </div>
    </div>
  );
}

function Section({ title, children }) {
  return (
    <div className={styles.section}>
      <div className={styles.sectionTitle}>{title}</div>
      <div className={styles.sectionBody}>{children}</div>
    </div>
  );
}

function Field({ label, sub, children, controlsClassName }) {
  return (
    <div className={styles.field}>
      <div className={styles.fieldInfo}>
        <div className={styles.fieldLabel}>{label}</div>
        {sub && <div className={styles.fieldSub}>{sub}</div>}
      </div>
      <div className={`${styles.fieldControls} ${controlsClassName || ''}`}>{children}</div>
    </div>
  );
}
