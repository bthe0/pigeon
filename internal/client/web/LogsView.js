function LogsView() {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-dim)' }}>
      <div style={{ textAlign: 'center' }}>
        <Icon d={Icons.log} size={32} color="var(--border2)" style={{marginBottom: 10}} />
        <p>Live remote streaming logs not yet implemented via Dashboard.</p>
        <p style={{fontSize:11, marginTop: 4}}>Use <code>pigeon logs</code> CLI.</p>
      </div>
    </div>
  );
}
