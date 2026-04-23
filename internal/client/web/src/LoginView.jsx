import { useState } from 'react';
import styles from './LoginView.module.css';

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
    <div className={styles.page}>
      <div className={styles.card}>
        <img src="/logo.png" alt="Pigeon logo" className={styles.logo} />
        <div className={styles.title}>pigeon</div>
        <div className={styles.subtitle}>Enter your dashboard password</div>

        <form onSubmit={handleSubmit} className={styles.form}>
          <div className={styles.fieldWrap}>
            <input
              type="password"
              placeholder="Password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className={styles.input}
            />
            {error && <div className={styles.error}>{error}</div>}
          </div>

          <button type="submit" disabled={loading} className={styles.submit}>
            {loading ? 'Authenticating...' : 'Sign In'}
          </button>
        </form>
      </div>
      <div className={styles.footer}>Self-hosted tunnel agent management</div>
    </div>
  );
}
