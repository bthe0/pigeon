import { useState, useRef, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { Pill, StatusDot, CopyBtn } from './Shared';
import { PROTO_COLORS } from './Constants';
import styles from './TunnelsView.module.css';

function SkeletonRow() {
  const bar = (w, opacity = 1) => (
    <div className={styles.skeletonBar} style={{ width: w, opacity }} />
  );
  return (
    <div className={`${styles.grid} ${styles.skeletonRow}`}>
      <div className={styles.skeletonDot} />
      {bar(20)}
      <div className={styles.skeletonStack}>{bar('60%')}{bar('40%', 0.6)}</div>
      {bar(36)}{bar(64)}{bar(28)}{bar(28)}{bar(56)}
    </div>
  );
}

export function TunnelsView({ tunnels, loading, reloadConfig, onSelectTunnel, baseDomain, dashFetch }) {
  const emptyForm = { localAddr: '', domain: '', port: '', proto: 'http', disabled: false, expose: 'both', httpPassword: '', maxConnections: '', unavailablePage: 'default', allowedIPs: '', captureBodies: false, staticRoot: '' };
  const localAddrRef = useRef(null);
  const [newOpen, setNewOpen] = useState(false);
  const [editId, setEditId] = useState(null);
  const [form, setForm] = useState(emptyForm);
  const [filter, setFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [deleteId, setDeleteId] = useState(null);
  const [isAdding, setIsAdding] = useState(false);
  const [formErrors, setFormErrors] = useState({});

  const filtered = tunnels.filter(t => {
    const matchStatus = filter === 'all' || t.status === filter;
    const matchSearch = !search || t.localPort.includes(search) || t.publicUrl.includes(search);
    return matchStatus && matchSearch;
  });

  useEffect(() => {
    if (!newOpen) return;
    requestAnimationFrame(() => {
      localAddrRef.current?.focus();
      localAddrRef.current?.select?.();
    });
  }, [newOpen, editId]);

  useEffect(() => {
    if (!newOpen) return;
    const onKeyDown = (e) => {
      if (e.key === 'Escape' && !isAdding) {
        e.preventDefault();
        setNewOpen(false);
        return;
      }
      if (e.key !== 'Enter' || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return;
      if (e.target && e.target.tagName === 'TEXTAREA') return;
      e.preventDefault();
      saveTunnel();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [newOpen, isAdding, form, editId]);

  async function confirmDelete() {
    if (!deleteId) return;
    try {
      const res = await dashFetch(`/api/forwards/${deleteId}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(await res.text());
      await new Promise(r => setTimeout(r, 150));
      await reloadConfig();
    } catch (err) {
      alert("Error deleting: " + err.message);
    }
    setDeleteId(null);
  }

  function parsePortValue(value) {
    if (!/^\d+$/.test(value)) return null;
    const port = parseInt(value, 10);
    if (port < 1 || port > 65535) return null;
    return port;
  }

  function isValidLocalAddr(value) {
    const trimmed = value.trim();
    if (!trimmed || /\s/.test(trimmed)) return false;
    const bracketed = trimmed.match(/^\[.+\]:(\d+)$/);
    if (bracketed) return parsePortValue(bracketed[1]) !== null;
    const idx = trimmed.lastIndexOf(':');
    if (idx <= 0 || idx === trimmed.length - 1) return false;
    return parsePortValue(trimmed.slice(idx + 1)) !== null;
  }

  function isValidDomain(value) {
    if (!value) return true;
    if (/\s/.test(value) || value.includes('://') || value.includes('/')) return false;
    return value.split('.').every((part, i) => {
      if (i === 0 && part === '*') return true;
      return /^[a-zA-Z0-9-]+$/.test(part) && !part.startsWith('-') && !part.endsWith('-');
    });
  }

  function parseAllowedIPs(value) {
    return (value || '').split(/[\n,]/).map(s => s.trim()).filter(Boolean);
  }

  function isValidCIDROrIP(value) {
    const s = value.trim();
    if (!s) return false;
    const cidr = s.match(/^([0-9a-fA-F:.]+)\/(\d+)$/);
    const host = cidr ? cidr[1] : s;
    const prefix = cidr ? parseInt(cidr[2], 10) : null;
    const isV4 = /^(\d{1,3}\.){3}\d{1,3}$/.test(host);
    const isV6 = host.includes(':') && /^[0-9a-fA-F:]+$/.test(host);
    if (!isV4 && !isV6) return false;
    if (isV4) {
      if (host.split('.').some(o => { const n = parseInt(o, 10); return isNaN(n) || n < 0 || n > 255; })) return false;
      if (cidr && (prefix < 0 || prefix > 32)) return false;
    } else if (cidr && (prefix < 0 || prefix > 128)) return false;
    return true;
  }

  function validateForm() {
    const errors = {};
    const localAddr = form.localAddr.trim();
    const isHTTP = form.proto === 'http' || form.proto === 'https';
    const isStatic = form.proto === 'static';
    const domain = form.domain.trim();
    const port = String(form.port || '').trim();
    const maxConnections = String(form.maxConnections || '').trim();
    const httpPassword = String(form.httpPassword || '');
    const staticRoot = (form.staticRoot || '').trim();

    if (isStatic) {
      if (!staticRoot) errors.staticRoot = 'Folder path is required.';
    } else if (!localAddr) {
      errors.localAddr = 'Local address is required.';
    } else if (!isValidLocalAddr(localAddr)) {
      errors.localAddr = 'Use host:port, for example localhost:3000.';
    }

    if (isHTTP || isStatic) {
      if (domain && !isValidDomain(domain)) {
        errors.domain = 'Use a hostname, like myapp or *.preview.example.com.';
      }
    } else if (port && parsePortValue(port) === null) {
      errors.port = 'Remote port must be between 1 and 65535.';
    }

    if (maxConnections && parsePortValue(maxConnections) === null) {
      errors.maxConnections = 'Max connections must be between 1 and 65535.';
    }

    if (isHTTP && httpPassword) {
      if (httpPassword.trim() !== httpPassword) {
        errors.httpPassword = 'Password cannot start or end with spaces.';
      } else if (httpPassword.length < 4) {
        errors.httpPassword = 'Password must be at least 4 characters.';
      } else if (httpPassword.length > 128) {
        errors.httpPassword = 'Password must be 128 characters or fewer.';
      }
    }

    const ips = parseAllowedIPs(form.allowedIPs);
    const bad = ips.filter(ip => !isValidCIDROrIP(ip));
    if (bad.length > 0) {
      errors.allowedIPs = `Invalid IP or CIDR: ${bad[0]}`;
    }

    return errors;
  }

  async function saveTunnel() {
    const errors = validateForm();
    setFormErrors(errors);
    if (Object.keys(errors).length > 0) return;

    setIsAdding(true);
    try {
      const localAddr = form.localAddr.trim();
      let domainVal = form.domain.trim() || undefined;
      if (domainVal && baseDomain) {
        const withoutStar = domainVal.startsWith('*.') ? domainVal.slice(2) : domainVal;
        if (!withoutStar.includes('.')) {
          domainVal = (domainVal.startsWith('*.') ? '*.' : '') + withoutStar + '.' + baseDomain;
        } else if (!domainVal.endsWith('.' + baseDomain) && domainVal !== baseDomain && !domainVal.endsWith(baseDomain)) {
          domainVal = `${domainVal}.${baseDomain}`;
        }
      }
      const remotePort = String(form.port || '').trim();
      const maxConnections = String(form.maxConnections || '').trim();
      const allowedIPs = parseAllowedIPs(form.allowedIPs);
      const payload = {
        protocol: form.proto,
        local_addr: form.proto === 'static' ? '' : localAddr,
        static_root: form.proto === 'static' ? (form.staticRoot || '').trim() : '',
        domain: domainVal,
        remote_port: remotePort ? parseInt(remotePort, 10) : 0,
        disabled: !!form.disabled,
        expose: form.expose || 'both',
        http_password: form.httpPassword || '',
        max_connections: maxConnections ? parseInt(maxConnections, 10) : 0,
        unavailable_page: form.unavailablePage || 'default',
        allowed_ips: allowedIPs,
        capture_bodies: !!form.captureBodies
      };
      const url = editId ? `/api/forwards/${editId}` : `/api/forwards`;
      const res = await dashFetch(url, {
        method: editId ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!res.ok) throw new Error(await res.text());

      setForm(emptyForm);
      setFormErrors({});
      setNewOpen(false);
      setEditId(null);
      await new Promise(r => setTimeout(r, 150));
      await reloadConfig();
    } catch (err) {
      alert("Error saving tunnel: " + err.message);
    }
    setIsAdding(false);
  }

  async function toggleTunnel(t) {
    try {
      const payload = {
        protocol: t.proto,
        local_addr: t.proto === 'static' ? '' : t.localPort,
        static_root: t.staticRoot || '',
        domain: t.domain || undefined,
        remote_port: t.remotePort ? parseInt(t.remotePort) : 0,
        disabled: !t.disabled,
        expose: t.expose || 'both',
        http_password: t.httpPassword || '',
        max_connections: t.maxConnections || 0,
        unavailable_page: t.unavailablePage || 'default',
        allowed_ips: t.allowedIPs || [],
        capture_bodies: !!t.captureBodies
      };
      const res = await dashFetch(`/api/forwards/${t.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!res.ok) throw new Error(await res.text());
      await new Promise(r => setTimeout(r, 150));
      await reloadConfig();
    } catch (err) {
      alert("Error toggling: " + err.message);
    }
  }

  async function cycleExpose(t) {
    const sslOn = t.expose !== 'http';
    const next = sslOn ? 'http' : 'https';
    try {
      const payload = {
        protocol: t.proto,
        local_addr: t.proto === 'static' ? '' : t.localPort,
        static_root: t.staticRoot || '',
        domain: t.domain || undefined,
        remote_port: t.remotePort ? parseInt(t.remotePort) : 0,
        disabled: !!t.disabled,
        expose: next,
        http_password: t.httpPassword || '',
        max_connections: t.maxConnections || 0,
        unavailable_page: t.unavailablePage || 'default',
        allowed_ips: t.allowedIPs || [],
        capture_bodies: !!t.captureBodies
      };
      const res = await dashFetch(`/api/forwards/${t.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!res.ok) throw new Error(await res.text());
      await new Promise(r => setTimeout(r, 150));
      await reloadConfig();
    } catch (err) {
      alert("Error updating expose: " + err.message);
    }
  }

  function openEdit(t) {
    setFormErrors({});
    setForm({
      localAddr: t.proto === 'static' ? '' : t.localPort,
      domain: t.domain || '',
      port: t.remotePort || '',
      proto: t.proto,
      disabled: !!t.disabled,
      expose: t.expose || 'both',
      httpPassword: t.httpPassword || '',
      maxConnections: t.maxConnections || '',
      unavailablePage: t.unavailablePage || 'default',
      allowedIPs: (t.allowedIPs || []).join('\n'),
      captureBodies: !!t.captureBodies,
      staticRoot: t.staticRoot || ''
    });
    setEditId(t.id);
    setNewOpen(true);
  }

  const statCounts = { all: tunnels.length, online: tunnels.filter(t => t.status === 'online').length, offline: tunnels.filter(t => t.status === 'offline').length };

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div className={styles.titleWrap}>
          <div className={styles.title}>Tunnels</div>
          <div className={styles.subtitle}>{statCounts.all} active forwards</div>
        </div>
        <input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search…" className={styles.search} />
        <button
          onClick={() => { setEditId(null); setForm(emptyForm); setFormErrors({}); setNewOpen(true); }}
          className={styles.newBtn}
        >
          <Icon d={Icons.plus} size={13} color="#000" /> New Tunnel
        </button>
      </div>

      <div className={styles.filters}>
        {['all', 'online', 'offline'].map(f => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={`${styles.filterBtn} ${filter === f ? styles.filterBtnActive : ''}`}
          >
            {f} <span className={styles.filterCount}>{statCounts[f]}</span>
          </button>
        ))}
      </div>

      <div className={`${styles.grid} ${styles.gridHeader}`}>
        {['', 'SSL', 'Local Target / URL', 'Proto', 'ID', 'Requests', 'Bandwidth', 'Actions'].map((h, i) => (
          <div key={i} className={styles.gridHeaderLabel}>{h}</div>
        ))}
      </div>

      <div className={styles.rows}>
        {loading ? (
          [1, 2, 3].map(i => <SkeletonRow key={i} />)
        ) : filtered.length === 0 ? (
          <div className={styles.empty}>No tunnels found</div>
        ) : filtered.map(t => (
          <TunnelRow key={t.id} tunnel={t} onDelete={setDeleteId} onToggle={toggleTunnel} onEdit={openEdit} onCycleExpose={cycleExpose} onClick={() => onSelectTunnel(t)} />
        ))}
      </div>

      {newOpen && (
        <div className={styles.modalBackdrop} onClick={() => !isAdding && setNewOpen(false)}>
          <form className={styles.modalForm} onClick={e => e.stopPropagation()} onSubmit={e => { e.preventDefault(); saveTunnel(); }}>
            <div className={styles.modalHeader}>
              <span className={styles.modalTitle}>{editId ? 'Edit Tunnel' : 'New Tunnel'}</span>
              <button type="button" disabled={isAdding} onClick={() => setNewOpen(false)} className={styles.closeBtn}>
                <Icon d={Icons.x} size={16} color="currentColor" />
              </button>
            </div>

            <div className={styles.formGrid}>
              <div>
                <label className={styles.label}>Protocol</label>
                <select value={form.proto} onChange={e => setForm(x => ({ ...x, proto: e.target.value }))} disabled={isAdding} className={styles.select}>
                  <option value="http">HTTP</option>
                  <option value="https">HTTPS (local TLS service)</option>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="static">Static (serve a folder)</option>
                </select>
              </div>

              {form.proto === 'static' ? (
                <div>
                  <label className={styles.label}>Static Folder</label>
                  <input
                    ref={localAddrRef}
                    value={form.staticRoot}
                    onChange={e => { const value = e.target.value; setForm(x => ({ ...x, staticRoot: value })); setFormErrors(x => ({ ...x, staticRoot: undefined })); }}
                    placeholder="/absolute/path/to/site"
                    disabled={isAdding}
                    className={styles.input}
                  />
                  {formErrors.staticRoot && <div className={styles.errorText}>{formErrors.staticRoot}</div>}
                </div>
              ) : (
                <div>
                  <label className={styles.label}>Local Address</label>
                  <input
                    ref={localAddrRef}
                    value={form.localAddr}
                    onChange={e => { const value = e.target.value; setForm(x => ({ ...x, localAddr: value })); setFormErrors(x => ({ ...x, localAddr: undefined })); }}
                    placeholder="localhost:3000"
                    disabled={isAdding}
                    className={styles.input}
                  />
                  {formErrors.localAddr && <div className={styles.errorText}>{formErrors.localAddr}</div>}
                </div>
              )}

              {(form.proto === 'http' || form.proto === 'https' || form.proto === 'static') ? (
                <div>
                  <label className={styles.label}>Domain (Optional)</label>
                  <div className={styles.domainRow}>
                    <input
                      value={form.domain}
                      onChange={e => { const value = e.target.value; setForm(x => ({ ...x, domain: value })); setFormErrors(x => ({ ...x, domain: undefined })); }}
                      placeholder={baseDomain ? 'myapp' : 'myapp.tunnel.dev'}
                      disabled={isAdding}
                      className={styles.domainInput}
                    />
                    {baseDomain && !form.domain.endsWith('.' + baseDomain) && !form.domain.endsWith(baseDomain) && (
                      <span className={styles.domainSuffix}>.{baseDomain}</span>
                    )}
                  </div>
                  {formErrors.domain && <div className={styles.errorText}>{formErrors.domain}</div>}
                </div>
              ) : (
                <div>
                  <label className={styles.label}>Remote Port (Optional)</label>
                  <input
                    type="number"
                    value={form.port}
                    onChange={e => { const value = e.target.value; setForm(x => ({ ...x, port: value })); setFormErrors(x => ({ ...x, port: undefined })); }}
                    placeholder="0 for auto assign"
                    disabled={isAdding}
                    className={styles.input}
                  />
                  {formErrors.port && <div className={styles.errorText}>{formErrors.port}</div>}
                </div>
              )}

              <div>
                <label className={styles.label}>Max Connections (Optional)</label>
                <input
                  type="number"
                  min="1"
                  value={form.maxConnections}
                  onChange={e => { const value = e.target.value; setForm(x => ({ ...x, maxConnections: value })); setFormErrors(x => ({ ...x, maxConnections: undefined })); }}
                  placeholder="Unlimited"
                  disabled={isAdding}
                  className={styles.input}
                />
                {formErrors.maxConnections && <div className={styles.errorText}>{formErrors.maxConnections}</div>}
              </div>

              {(form.proto === 'http' || form.proto === 'https') && (
                <>
                  <div>
                    <label className={styles.label}>HTTP Password (Optional)</label>
                    <input
                      type="password"
                      value={form.httpPassword}
                      onChange={e => { const value = e.target.value; setForm(x => ({ ...x, httpPassword: value })); setFormErrors(x => ({ ...x, httpPassword: undefined })); }}
                      placeholder="Protect with tunnel password"
                      disabled={isAdding}
                      className={styles.input}
                    />
                    {formErrors.httpPassword && <div className={styles.errorText}>{formErrors.httpPassword}</div>}
                  </div>

                  <div>
                    <label className={styles.label}>Unavailable Page</label>
                    <select value={form.unavailablePage} onChange={e => setForm(x => ({ ...x, unavailablePage: e.target.value }))} disabled={isAdding} className={styles.select}>
                      <option value="default">Default</option>
                      <option value="minimal">Minimal</option>
                      <option value="terminal">Terminal</option>
                    </select>
                  </div>
                </>
              )}

              <div className={styles.formFull}>
                <label className={styles.label}>Allowed IPs (Optional)</label>
                <textarea
                  value={form.allowedIPs}
                  onChange={e => { const value = e.target.value; setForm(x => ({ ...x, allowedIPs: value })); setFormErrors(x => ({ ...x, allowedIPs: undefined })); }}
                  placeholder={`10.0.0.0/8\n203.0.113.5`}
                  disabled={isAdding}
                  rows={3}
                  className={styles.textarea}
                />
                <div className={styles.hint}>One IP or CIDR per line. Empty = allow all.</div>
                {formErrors.allowedIPs && <div className={styles.errorText}>{formErrors.allowedIPs}</div>}
              </div>

              {(form.proto === 'http' || form.proto === 'https') && (
                <div className={`${styles.formFull} ${styles.checkboxRow}`}>
                  <input type="checkbox" id="capture_bodies" checked={!!form.captureBodies} onChange={e => setForm(x => ({ ...x, captureBodies: e.target.checked }))} disabled={isAdding} />
                  <label htmlFor="capture_bodies" className={styles.checkboxLabel}>Capture request/response bodies in inspector (up to 256&nbsp;KB)</label>
                </div>
              )}
            </div>

            <button type="submit" disabled={isAdding} className={styles.submit}>
              {isAdding ? 'Saving...' : (editId ? 'Save Tunnel' : 'Start Tunnel')}
            </button>
          </form>
        </div>
      )}

      {deleteId && (
        <div className={styles.modalBackdrop} onClick={() => setDeleteId(null)}>
          <div className={styles.confirmCard} onClick={e => e.stopPropagation()}>
            <div className={styles.confirmTitle}>Delete Forward</div>
            <div className={styles.confirmBody}>Are you sure you want to delete this forward? This action cannot be undone.</div>
            <div className={styles.confirmButtons}>
              <button onClick={() => setDeleteId(null)} className={styles.confirmCancel}>Cancel</button>
              <button onClick={confirmDelete} className={styles.confirmDanger}>Delete</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function TunnelRow({ tunnel: t, onDelete, onToggle, onEdit, onCycleExpose, onClick }) {
  const [hovered, setHovered] = useState(false);
  const isHTTP = t.proto === 'http' || t.proto === 'https';
  const sslOn = isHTTP && t.expose !== 'http';
  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onClick={onClick}
      className={`${styles.grid} ${styles.tunnelRow} ${hovered ? styles.tunnelRowHover : ''} ${t.disabled ? styles.tunnelRowDisabled : ''}`}
    >
      <StatusDot status={t.status} />
      <div>
        {isHTTP && (
          <button
            onClick={e => { e.stopPropagation(); onCycleExpose(t); }}
            title={sslOn ? 'SSL on — click to disable' : 'SSL off — click to enable'}
            className={`${styles.sslBtn} ${sslOn ? styles.sslBtnOn : ''}`}
          >
            <Icon d={Icons.lock} size={11} color="currentColor" />
          </button>
        )}
      </div>
      <div>
        <div className={styles.rowLine}>
          <span className={styles.localPort}>{t.localPort}</span>
          {t.tags.map(tag => <Pill key={tag} color={tag === 'prod' ? '#ff4d4d' : tag === 'db' ? '#f5c542' : '#4d9fff'}>{tag}</Pill>)}
        </div>
        <div className={styles.urlLine}>
          {t.publicUrl
            ? <>
                <a
                  href={`${t.urlScheme}://${t.publicUrl}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={e => e.stopPropagation()}
                  className={styles.urlLink}
                >
                  {t.urlScheme}://{t.publicUrl}
                </a>
                <CopyBtn text={`${t.urlScheme}://${t.publicUrl}`} />
              </>
            : <span className={styles.urlAuto}>auto-assigned</span>
          }
          {!!t.httpPassword && <span><Icon d={Icons.lock} size={11} color="var(--yellow)" title="Protected with HTTP basic auth" /></span>}
          {t.status === 'online' && t.latency && (
            <span className={styles.latency}>{t.latency}ms</span>
          )}
          {t.isLocal && <span className={styles.localPill}><Pill color="#7c5cfc">local</Pill></span>}
        </div>
      </div>
      <div><Pill color={PROTO_COLORS[t.proto] || '#9ba39c'}>{t.proto}</Pill></div>
      <div className={styles.id}>{t.id}</div>
      <div className={styles.metric}>{t.requests.toLocaleString()}</div>
      <div className={styles.metric}>{t.bandwidth}</div>
      <div className={styles.actions}>
        <button
          onClick={e => { e.stopPropagation(); onToggle(t); }}
          className={`${styles.iconBtn} ${t.disabled ? '' : styles.iconBtnAccent}`}
        >
          <Icon d={t.disabled ? Icons.toggleOff : Icons.toggleOn} size={13} color="currentColor" />
        </button>
        <button
          onClick={e => { e.stopPropagation(); onEdit(t); }}
          className={styles.iconBtn}
        >
          <Icon d={Icons.edit} size={11} color="currentColor" />
        </button>
        <button
          onClick={e => { e.stopPropagation(); onDelete(t.id); }}
          className={`${styles.iconBtn} ${styles.iconBtnDanger}`}
        >
          <Icon d={Icons.trash} size={11} color="#ff4d4d" />
        </button>
      </div>
    </div>
  );
}
