import React, { useState, useRef, useEffect } from 'react';
import { Icon, Icons } from './Icons';
import { Pill, StatusDot, CopyBtn } from './Shared';
import { PROTO_COLORS } from './Constants';

function SkeletonRow() {
  const skel = (w, opacity = 1) => (
    <div style={{ height: 10, width: w, background: 'var(--border2)', borderRadius: 2, animation: 'shimmer 1.6s ease infinite', opacity }} />
  );
  return (
    <div className="tunnels-grid" style={{ display: 'grid', gridTemplateColumns: '16px 50px 1fr 80px 100px 90px 90px 90px', gap: '0 12px', padding: '14px 24px', borderBottom: '1px solid var(--border)', alignItems: 'center' }}>
      <div style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--border2)', animation: 'shimmer 1.6s ease infinite' }} />
      {skel(20)}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>{skel('60%')}{skel('40%', 0.6)}</div>
      {skel(36)}{skel(64)}{skel(28)}{skel(28)}{skel(56)}
    </div>
  );
}

export function TunnelsView({ tunnels, loading, reloadConfig, onSelectTunnel, baseDomain }) {
  const emptyForm = { localAddr: '', domain: '', port: '', proto: 'http', disabled: false, expose: 'both', httpPassword: '', maxConnections: '', unavailablePage: 'default' };
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
        return
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
      const res = await fetch(`/api/forwards/${deleteId}`, { method: 'DELETE' });
      if(!res.ok) throw new Error(await res.text());
      await reloadConfig();
    } catch(err) {
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
    return value.split('.').every(part => /^[a-zA-Z0-9-]+$/.test(part) && !part.startsWith('-') && !part.endsWith('-'));
  }

  function validateForm() {
    const errors = {};
    const localAddr = form.localAddr.trim();
    const isHTTP = form.proto === 'http' || form.proto === 'https';
    const domain = form.domain.trim();
    const port = String(form.port || '').trim();
    const maxConnections = String(form.maxConnections || '').trim();
    const httpPassword = String(form.httpPassword || '');

    if (!localAddr) {
      errors.localAddr = 'Local address is required.';
    } else if (!isValidLocalAddr(localAddr)) {
      errors.localAddr = 'Use host:port, for example localhost:3000.';
    }

    if (isHTTP) {
      if (domain && !isValidDomain(domain)) {
        errors.domain = 'Use a hostname only, like myapp or myapp.pigeon.local.';
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
      if (domainVal && baseDomain && !domainVal.includes('.')) {
        domainVal = `${domainVal}.${baseDomain}`;
      }
      const remotePort = String(form.port || '').trim();
      const maxConnections = String(form.maxConnections || '').trim();
      const payload = {
        protocol: form.proto,
        local_addr: localAddr,
        domain: domainVal,
        remote_port: remotePort ? parseInt(remotePort, 10) : 0,
        disabled: !!form.disabled,
        expose: form.expose || 'both',
        http_password: form.httpPassword || '',
        max_connections: maxConnections ? parseInt(maxConnections, 10) : 0,
        unavailable_page: form.unavailablePage || 'default'
      };
      const url = editId ? `/api/forwards/${editId}` : `/api/forwards`;
      const res = await fetch(url, {
        method: editId ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if(!res.ok) throw new Error(await res.text());
      
      setForm(emptyForm);
      setFormErrors({});
      setNewOpen(false);
      setEditId(null);
      await reloadConfig();
    } catch(err) {
      alert("Error saving tunnel: " + err.message);
    }
    setIsAdding(false);
  }

  async function toggleTunnel(t) {
    try {
      const payload = {
        protocol: t.proto,
        local_addr: t.localPort,
        domain: t.domain || undefined,
        remote_port: t.remotePort ? parseInt(t.remotePort) : 0,
        disabled: !t.disabled,
        expose: t.expose || 'both',
        http_password: t.httpPassword || '',
        max_connections: t.maxConnections || 0,
        unavailable_page: t.unavailablePage || 'default'
      };
      const res = await fetch(`/api/forwards/${t.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if(!res.ok) throw new Error(await res.text());
      await reloadConfig();
    } catch(err) {
      alert("Error toggling: " + err.message);
    }
  }

  async function cycleExpose(t) {
    const sslOn = t.expose !== 'http';
    const next = sslOn ? 'http' : 'both';
    try {
      const payload = {
        protocol: t.proto,
        local_addr: t.localPort,
        domain: t.domain || undefined,
        remote_port: t.remotePort ? parseInt(t.remotePort) : 0,
        disabled: !!t.disabled,
        expose: next,
        http_password: t.httpPassword || '',
        max_connections: t.maxConnections || 0,
        unavailable_page: t.unavailablePage || 'default'
      };
      const res = await fetch(`/api/forwards/${t.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if(!res.ok) throw new Error(await res.text());
      await reloadConfig();
    } catch(err) {
      alert("Error updating expose: " + err.message);
    }
  }

  function openEdit(t) {
    setFormErrors({});
    setForm({ localAddr: t.localPort, domain: t.domain || '', port: t.remotePort || '', proto: t.proto, disabled: !!t.disabled, expose: t.expose || 'both', httpPassword: t.httpPassword || '', maxConnections: t.maxConnections || '', unavailablePage: t.unavailablePage || 'default' });
    setEditId(t.id);
    setNewOpen(true);
  }

  const statCounts = { all: tunnels.length, online: tunnels.filter(t=>t.status==='online').length, offline: tunnels.filter(t=>t.status==='offline').length };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0 }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: '#fff', letterSpacing: '-.01em' }}>Tunnels</div>
          <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>{statCounts.all} active forwards</div>
        </div>
        <input value={search} onChange={e=>setSearch(e.target.value)} placeholder="Search…"
          style={{ background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '6px 10px', fontSize: 12, fontFamily: 'var(--sans)', width: 160, outline: 'none' }} />
        <button onClick={() => { setEditId(null); setForm(emptyForm); setFormErrors({}); setNewOpen(true); }}
          style={{ display: 'flex', alignItems: 'center', gap: 6, background: 'var(--accent)', border: 'none', color: '#000', padding: '7px 14px', fontSize: 12, fontWeight: 600, cursor: 'pointer', letterSpacing: '.02em' }}>
          <Icon d={Icons.plus} size={13} color="#000" /> New Tunnel
        </button>
      </div>

      <div style={{ display: 'flex', gap: 0, padding: '0 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        {['all', 'online', 'offline'].map(f => (
          <button key={f} onClick={()=>setFilter(f)}
            style={{ padding: '8px 14px', background: 'none', border: 'none', borderBottom: `2px solid ${filter===f?'var(--accent)':'transparent'}`, color: filter===f?'var(--accent)':'var(--text-dim)', fontSize: 12, cursor: 'pointer', fontFamily: 'var(--sans)', fontWeight: filter===f?500:400, textTransform: 'capitalize', marginBottom: -1, transition: 'all .12s' }}>
            {f} <span style={{ fontFamily: 'var(--mono)', fontSize: 10, marginLeft: 3, color: 'inherit', opacity: .7 }}>{statCounts[f]}</span>
          </button>
        ))}
      </div>

      <div className="tunnels-grid" style={{ display: 'grid', gridTemplateColumns: '16px 50px 1fr 80px 100px 90px 90px 90px', gap: '0 12px', padding: '6px 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        {['', 'SSL', 'Local Target / URL', 'Proto', 'ID', 'Requests', 'Bandwidth', 'Actions'].map((h,i) => (
          <div key={i} style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>{h}</div>
        ))}
      </div>

      <div style={{ flex: 1, overflowY: 'auto' }}>
        {loading ? (
          [1,2,3].map(i => <SkeletonRow key={i} />)
        ) : filtered.length === 0 ? (
          <div style={{ padding: '40px 24px', textAlign: 'center', color: 'var(--text-dim)', fontSize: 13 }}>No tunnels found</div>
        ) : filtered.map(t => (
          <TunnelRow key={t.id} tunnel={t} onDelete={setDeleteId} onToggle={toggleTunnel} onEdit={openEdit} onCycleExpose={cycleExpose} onClick={() => onSelectTunnel(t)} />
        ))}
      </div>

      {newOpen && (
        <div style={{ position: 'absolute', inset: 0, background: '#00000088', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100 }}
          onClick={() => !isAdding && setNewOpen(false)}>
          <form className="modal-form" style={{ background: 'var(--panel)', border: '1px solid var(--border2)', width: 420, padding: 24 }} onClick={e=>e.stopPropagation()} onSubmit={e => { e.preventDefault(); saveTunnel(); }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <span style={{ fontSize: 14, fontWeight: 600, color: '#fff' }}>{editId ? 'Edit Tunnel' : 'New Tunnel'}</span>
              <button type="button" disabled={isAdding} onClick={() => setNewOpen(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-dim)' }}><Icon d={Icons.x} size={16} color="currentColor" /></button>
            </div>
            
            <div style={{ marginBottom: 14 }}>
              <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Protocol</label>
              <select value={form.proto} onChange={e => setForm(x => ({...x, proto: e.target.value}))} disabled={isAdding}
                style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }}>
                <option value="http">HTTP</option>
                <option value="https">HTTPS (local TLS service)</option>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </div>
            
            <div style={{ marginBottom: 14 }}>
              <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Local Address</label>
              <input ref={localAddrRef} value={form.localAddr} onChange={e => { const value = e.target.value; setForm(x => ({...x, localAddr: value})); setFormErrors(x => ({ ...x, localAddr: undefined })); }} placeholder="localhost:3000" disabled={isAdding}
                style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
              {formErrors.localAddr && <div style={{ marginTop: 6, fontSize: 11, color: 'var(--red)' }}>{formErrors.localAddr}</div>}
            </div>
            
            {(form.proto === 'http' || form.proto === 'https') ? (
              <div style={{ marginBottom: 14 }}>
                <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Domain (Optional)</label>
                <div style={{ display: 'flex', alignItems: 'center', border: '1px solid var(--border2)', background: 'var(--panel2)' }}>
                  <input value={form.domain} onChange={e => { const value = e.target.value; setForm(x => ({...x, domain: value})); setFormErrors(x => ({ ...x, domain: undefined })); }} placeholder={baseDomain ? 'myapp' : 'myapp.tunnel.dev'} disabled={isAdding}
                    style={{ flex: 1, background: 'transparent', border: 'none', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
                  {baseDomain && !form.domain.includes('.') && (
                    <span style={{ fontFamily: 'var(--mono)', fontSize: 13, color: 'var(--text-dim)', padding: '8px 10px 8px 0', whiteSpace: 'nowrap' }}>.{baseDomain}</span>
                  )}
                </div>
                {formErrors.domain && <div style={{ marginTop: 6, fontSize: 11, color: 'var(--red)' }}>{formErrors.domain}</div>}
              </div>
            ) : (
              <div style={{ marginBottom: 14 }}>
                <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Remote Port (Optional)</label>
                <input type="number" value={form.port} onChange={e => { const value = e.target.value; setForm(x => ({...x, port: value})); setFormErrors(x => ({ ...x, port: undefined })); }} placeholder="0 for auto assign" disabled={isAdding}
                  style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
                {formErrors.port && <div style={{ marginTop: 6, fontSize: 11, color: 'var(--red)' }}>{formErrors.port}</div>}
              </div>
            )}

            <div style={{ marginBottom: 14 }}>
              <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Max Connections (Optional)</label>
              <input type="number" min="1" value={form.maxConnections} onChange={e => { const value = e.target.value; setForm(x => ({...x, maxConnections: value})); setFormErrors(x => ({ ...x, maxConnections: undefined })); }} placeholder="Unlimited" disabled={isAdding}
                style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
              {formErrors.maxConnections && <div style={{ marginTop: 6, fontSize: 11, color: 'var(--red)' }}>{formErrors.maxConnections}</div>}
            </div>

            {(form.proto === 'http' || form.proto === 'https') && (
              <>
                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>HTTP Password (Optional)</label>
                  <input type="password" value={form.httpPassword} onChange={e => { const value = e.target.value; setForm(x => ({...x, httpPassword: value})); setFormErrors(x => ({ ...x, httpPassword: undefined })); }} placeholder="Protect with tunnel password" disabled={isAdding}
                    style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
                  {formErrors.httpPassword && <div style={{ marginTop: 6, fontSize: 11, color: 'var(--red)' }}>{formErrors.httpPassword}</div>}
                </div>

                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Unavailable Page</label>
                  <select value={form.unavailablePage} onChange={e => setForm(x => ({...x, unavailablePage: e.target.value}))} disabled={isAdding}
                    style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }}>
                    <option value="default">Default</option>
                    <option value="minimal">Minimal</option>
                    <option value="terminal">Terminal</option>
                  </select>
                </div>
              </>
            )}
            
            <button type="submit" disabled={isAdding}
              style={{ width: '100%', background: isAdding ? 'var(--accent-mid)' : 'var(--accent)', border: 'none', color: '#000', padding: '10px', fontSize: 13, fontWeight: 600, cursor: 'pointer', letterSpacing: '.03em', marginTop: 10 }}>
              {isAdding ? 'Saving...' : (editId ? 'Save Tunnel' : 'Start Tunnel')}
            </button>
          </form>
        </div>
      )}

      {deleteId && (
        <div style={{ position: 'absolute', inset: 0, background: '#00000088', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100 }}
          onClick={() => setDeleteId(null)}>
          <div style={{ background: 'var(--panel)', border: '1px solid var(--border2)', width: 360, padding: 24 }} onClick={e=>e.stopPropagation()}>
            <div style={{ fontSize: 15, fontWeight: 600, color: '#fff', marginBottom: 10 }}>Delete Forward</div>
            <div style={{ fontSize: 13, color: 'var(--text-dim)', marginBottom: 20 }}>Are you sure you want to delete this forward? This action cannot be undone.</div>
            <div style={{ display: 'flex', gap: 10 }}>
              <button onClick={() => setDeleteId(null)}
                style={{ flex: 1, background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>
                Cancel
              </button>
              <button onClick={confirmDelete}
                style={{ flex: 1, background: '#ff4d4d', border: 'none', color: '#000', padding: '8px', fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                Delete
              </button>
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
    <div className="tunnels-grid" onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)} onClick={onClick}
      style={{ display: 'grid', gridTemplateColumns: '16px 50px 1fr 80px 100px 90px 90px 90px', gap: '0 12px', padding: '10px 24px', borderBottom: '1px solid var(--border)', cursor: 'pointer', background: hovered ? 'var(--panel2)' : 'transparent', transition: 'background .1s', alignItems: 'center', opacity: t.disabled ? 0.6 : 1 }}>
      <StatusDot status={t.status} />
      <div>
        {isHTTP && (
          <button onClick={e=>{e.stopPropagation();onCycleExpose(t);}} title={sslOn ? 'SSL on — click to disable' : 'SSL off — click to enable'}
            style={{ background: 'none', border: `1px solid ${sslOn ? '#51d88a' : 'var(--border2)'}`, padding: '4px 6px', cursor: 'pointer', color: sslOn ? '#51d88a' : 'var(--text-dim)', display: 'flex', alignItems: 'center' }}>
            <Icon d={Icons.lock} size={11} color="currentColor" />
          </button>
        )}
      </div>
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: '#fff' }}>{t.localPort}</span>
          {t.tags.map(tag => <Pill key={tag} color={tag==='prod'?'#ff4d4d':tag==='db'?'#f5c542':'#4d9fff'}>{tag}</Pill>)}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 2 }}>
          {t.publicUrl
            ? <><span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)' }}>{t.urlScheme}://{t.publicUrl}</span><CopyBtn text={`${t.urlScheme}://${t.publicUrl}`} /></>
            : <span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)', fontStyle: 'italic' }}>auto-assigned</span>
          }
          {!!t.httpPassword && <span><Icon d={Icons.lock} size={11} color="var(--yellow)" title="Protected with HTTP basic auth" /></span>}
          {t.status === 'online' && t.latency && (
            <span style={{ fontFamily: 'var(--mono)', fontSize: 10, color: 'var(--accent)', marginLeft: 4 }}>{t.latency}ms</span>
          )}
          {t.isLocal && <span style={{ marginLeft: 8 }}><Pill color="#7c5cfc">local</Pill></span>}
        </div>
      </div>
      <div><Pill color={PROTO_COLORS[t.proto] || '#9ba39c'}>{t.proto}</Pill></div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)', letterSpacing: '1px' }}>{t.id}</div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text-mid)' }}>{t.requests.toLocaleString()}</div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text-mid)' }}>{t.bandwidth}</div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <button onClick={e=>{e.stopPropagation();onToggle(t);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: t.disabled ? 'var(--text-dim)' : 'var(--accent)', display: 'flex', alignItems: 'center' }}>
          <Icon d={t.disabled ? Icons.toggleOff : Icons.toggleOn} size={13} color="currentColor" />
        </button>
        <button onClick={e=>{e.stopPropagation();onEdit(t);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: 'var(--text-dim)', display: 'flex', alignItems: 'center' }}>
          <Icon d={Icons.edit} size={11} color="currentColor" />
        </button>
        <button onClick={e=>{e.stopPropagation();onDelete(t.id);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: '#ff4d4d88', display: 'flex', alignItems: 'center' }}>
          <Icon d={Icons.trash} size={11} color="#ff4d4d" />
        </button>
      </div>
    </div>
  );
}

