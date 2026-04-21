function TunnelsView({ tunnels, reloadConfig, onSelectTunnel }) {
  const [newOpen, setNewOpen] = useState(false);
  const [editId, setEditId] = useState(null);
  const [form, setForm] = useState({ localAddr: '', domain: '', port: '', proto: 'http', disabled: false });
  const [filter, setFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [deleteId, setDeleteId] = useState(null);
  const [isAdding, setIsAdding] = useState(false);

  const filtered = tunnels.filter(t => {
    const matchStatus = filter === 'all' || t.status === filter;
    const matchSearch = !search || t.localPort.includes(search) || t.publicUrl.includes(search);
    return matchStatus && matchSearch;
  });

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

  async function saveTunnel() {
    if (!form.localAddr) return;
    setIsAdding(true);
    try {
      const payload = {
        protocol: form.proto,
        local_addr: form.localAddr,
        domain: form.domain || undefined,
        remote_port: form.port ? parseInt(form.port) : 0,
        disabled: !!form.disabled
      };
      const url = editId ? `/api/forwards/${editId}` : `/api/forwards`;
      const res = await fetch(url, {
        method: editId ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if(!res.ok) throw new Error(await res.text());
      
      setForm({ localAddr: '', domain: '', port: '', proto: 'http', disabled: false });
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
        disabled: !t.disabled
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

  function openEdit(t) {
    setForm({ localAddr: t.localPort, domain: t.domain || '', port: t.remotePort || '', proto: t.proto, disabled: !!t.disabled });
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
        <button onClick={() => { setEditId(null); setForm({ localAddr: '', domain: '', port: '', proto: 'http', disabled: false }); setNewOpen(true); }}
          style={{ display: 'flex', alignItems: 'center', gap: 6, background: 'var(--accent)', border: 'none', color: '#000', padding: '7px 14px', fontSize: 12, fontWeight: 600, cursor: 'pointer', letterSpacing: '.02em' }}>
          <Icon d={Icons.plus} size={13} color="#000" /> New Tunnel
        </button>
      </div>

      <div style={{ display: 'flex', gap: 0, padding: '0 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        {['all'].map(f => (
          <button key={f} onClick={()=>setFilter(f)}
            style={{ padding: '8px 14px', background: 'none', border: 'none', borderBottom: `2px solid ${filter===f?'var(--accent)':'transparent'}`, color: filter===f?'var(--accent)':'var(--text-dim)', fontSize: 12, cursor: 'pointer', fontFamily: 'var(--sans)', fontWeight: filter===f?500:400, textTransform: 'capitalize', marginBottom: -1, transition: 'all .12s' }}>
            {f} <span style={{ fontFamily: 'var(--mono)', fontSize: 10, marginLeft: 3, color: 'inherit', opacity: .7 }}>{statCounts[f]}</span>
          </button>
        ))}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '16px 1fr 80px 100px 90px 90px 100px', gap: '0 12px', padding: '6px 24px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        {['', 'Local Target / URL', 'Proto', 'ID', 'Requests', 'Bandwidth', 'Actions'].map((h,i) => (
          <div key={i} style={{ fontSize: 10, fontWeight: 600, letterSpacing: '.07em', textTransform: 'uppercase', color: 'var(--text-dim)' }}>{h}</div>
        ))}
      </div>

      <div style={{ flex: 1, overflowY: 'auto' }}>
        {filtered.map(t => (
          <TunnelRow key={t.id} tunnel={t} onDelete={setDeleteId} onToggle={toggleTunnel} onEdit={openEdit} onClick={() => onSelectTunnel(t)} />
        ))}
        {filtered.length === 0 && (
          <div style={{ padding: '40px 24px', textAlign: 'center', color: 'var(--text-dim)', fontSize: 13 }}>No tunnels found</div>
        )}
      </div>

      {newOpen && (
        <div style={{ position: 'absolute', inset: 0, background: '#00000088', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100 }}
          onClick={() => !isAdding && setNewOpen(false)}>
          <div style={{ background: 'var(--panel)', border: '1px solid var(--border2)', width: 420, padding: 24 }} onClick={e=>e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <span style={{ fontSize: 14, fontWeight: 600, color: '#fff' }}>{editId ? 'Edit Tunnel' : 'New Tunnel'}</span>
              <button disabled={isAdding} onClick={() => setNewOpen(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-dim)' }}><Icon d={Icons.x} size={16} color="currentColor" /></button>
            </div>
            
            <div style={{ marginBottom: 14 }}>
              <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Protocol</label>
              <select value={form.proto} onChange={e => setForm(x => ({...x, proto: e.target.value}))} disabled={isAdding}
                style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }}>
                <option value="http">HTTP</option>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </div>
            
            <div style={{ marginBottom: 14 }}>
              <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Local Address</label>
              <input value={form.localAddr} onChange={e => setForm(x => ({...x, localAddr: e.target.value}))} placeholder="localhost:3000" disabled={isAdding}
                style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
            </div>
            
            {form.proto === 'http' ? (
              <div style={{ marginBottom: 14 }}>
                <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Domain (Optional)</label>
                <input value={form.domain} onChange={e => setForm(x => ({...x, domain: e.target.value}))} placeholder="myapp.tunnel.dev" disabled={isAdding}
                  style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
              </div>
            ) : (
              <div style={{ marginBottom: 14 }}>
                <label style={{ display: 'block', fontSize: 11, color: 'var(--text-dim)', marginBottom: 4, letterSpacing: '.05em', textTransform: 'uppercase' }}>Remote Port (Optional)</label>
                <input type="number" value={form.port} onChange={e => setForm(x => ({...x, port: e.target.value}))} placeholder="0 for auto assign" disabled={isAdding}
                  style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '8px 10px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }} />
              </div>
            )}
            
            <button onClick={saveTunnel} disabled={isAdding}
              style={{ width: '100%', background: isAdding ? 'var(--accent-mid)' : 'var(--accent)', border: 'none', color: '#000', padding: '10px', fontSize: 13, fontWeight: 600, cursor: 'pointer', letterSpacing: '.03em', marginTop: 10 }}>
              {isAdding ? 'Saving...' : (editId ? 'Save Tunnel' : 'Start Tunnel')}
            </button>
          </div>
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

function TunnelRow({ tunnel: t, onDelete, onToggle, onEdit, onClick }) {
  const [hovered, setHovered] = useState(false);
  return (
    <div onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)} onClick={onClick}
      style={{ display: 'grid', gridTemplateColumns: '16px 1fr 80px 100px 90px 90px 100px', gap: '0 12px', padding: '10px 24px', borderBottom: '1px solid var(--border)', cursor: 'pointer', background: hovered ? 'var(--panel2)' : 'transparent', transition: 'background .1s', alignItems: 'center', opacity: t.disabled ? 0.6 : 1 }}>
      <StatusDot status={t.status} />
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: '#fff' }}>{t.localPort}</span>
          {t.tags.map(tag => <Pill key={tag} color={tag==='prod'?'#ff4d4d':tag==='db'?'#f5c542':'#4d9fff'}>{tag}</Pill>)}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 2 }}>
          <span style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)' }}>{t.proto}://{t.publicUrl}</span>
          <CopyBtn text={`${t.proto}://${t.publicUrl}`} />
        </div>
      </div>
      <div><Pill color={PROTO_COLORS[t.proto] || '#9ba39c'}>{t.proto}</Pill></div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--text-dim)', letterSpacing: '1px' }}>{t.id}</div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text-mid)' }}>{t.requests.toLocaleString()}</div>
      <div style={{ fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text-mid)' }}>{t.bandwidth}</div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <button onClick={e=>{e.stopPropagation();onToggle(t);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: t.disabled ? 'var(--text-dim)' : 'var(--accent)', display: 'flex', alignItems: 'center', title: t.disabled ? 'Enable' : 'Disable' }}>
          <Icon d={t.disabled ? Icons.toggleOff : Icons.toggleOn} size={13} color="currentColor" />
        </button>
        <button onClick={e=>{e.stopPropagation();onEdit(t);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: 'var(--text-dim)', display: 'flex', alignItems: 'center', title: 'Edit' }}>
          <Icon d={Icons.edit} size={11} color="currentColor" />
        </button>
        <button onClick={e=>{e.stopPropagation();onDelete(t.id);}}
          style={{ background: 'none', border: '1px solid var(--border2)', padding: '4px 6px', cursor: 'pointer', color: '#ff4d4d88', display: 'flex', alignItems: 'center', title: 'Delete' }}>
          <Icon d={Icons.trash} size={11} color="#ff4d4d" />
        </button>
      </div>
    </div>
  );
}
