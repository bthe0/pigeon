function InspectorView({ tunnels }) {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-dim)' }}>
      <div style={{ textAlign: 'center' }}>
        <Icon d={Icons.activity} size={32} color="var(--border2)" style={{marginBottom: 10}} />
        <p>HTTP Inspector not natively supported by Pigeon yet.</p>
      </div>
    </div>
  );
}
