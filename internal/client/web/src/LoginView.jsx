import React, { useState } from 'react';
import { Icon, Icons } from './Icons';

export function LoginView({ onLogin }) {
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      });
      if (res.ok) {
        onLogin();
      } else {
        setError('Invalid password. Please try again.');
      }
    } catch (err) {
      setError('Connection failed. Is the daemon running?');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg)', color: '#fff', flexDirection: 'column' }}>
      <div style={{ width: 320, background: 'var(--panel)', border: '1px solid var(--border)', padding: 32, display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
        <Icon d={Icons.tunnel} size={40} color="var(--accent)" />
        <div style={{ fontSize: 20, fontWeight: 600, marginTop: 16, marginBottom: 8, letterSpacing: '.04em' }}>pigeon</div>
        <div style={{ fontSize: 12, color: 'var(--text-dim)', marginBottom: 32 }}>Enter your dashboard password</div>

        <form onSubmit={handleSubmit} style={{ width: '100%' }}>
          <div style={{ marginBottom: 20 }}>
            <input
              type="password"
              placeholder="Dashboard Password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              style={{ width: '100%', background: 'var(--panel2)', border: '1px solid var(--border2)', color: 'var(--text)', padding: '10px 14px', fontSize: 13, fontFamily: 'var(--mono)', outline: 'none' }}
            />
            {error && <div style={{ color: 'var(--red)', fontSize: 11, marginTop: 8 }}>{error}</div>}
          </div>

          <button
            type="submit"
            disabled={loading}
            style={{ width: '100%', background: 'var(--accent)', border: 'none', color: '#000', padding: '10px', fontSize: 14, fontWeight: 600, cursor: 'pointer', transition: 'all .2s', opacity: loading ? 0.7 : 1 }}
          >
            {loading ? 'Authenticating...' : 'Sign In'}
          </button>
        </form>
      </div>
      <div style={{ marginTop: 24, fontSize: 11, color: 'var(--text-dim)', fontFamily: 'var(--mono)' }}>
        Self-hosted tunnel agent management
      </div>
    </div>
  );
}
