import { useState } from 'react';
import { Icon, Icons } from './Icons';
import { STATUS_COLORS } from './Constants';
import styles from './Shared.module.css';

export function Pill({ children, color }) {
  return (
    <span
      className={styles.pill}
      style={{ border: `1px solid ${color}44`, color, background: color + '14' }}
    >
      {children}
    </span>
  );
}

export function StatusDot({ status }) {
  const color = STATUS_COLORS[status] || '#6b7068';
  const online = status === 'online';
  return (
    <span className={styles.statusWrap}>
      <span
        className={`${styles.statusDot} ${online ? styles.statusDotOnline : ''}`}
        style={{ background: color, boxShadow: online ? `0 0 6px ${color}` : 'none' }}
      />
    </span>
  );
}

export function CopyBtn({ text }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      className={`${styles.copyBtn} ${copied ? styles.copyBtnCopied : ''}`}
      onClick={e => {
        e.stopPropagation();
        navigator.clipboard?.writeText(text).catch(() => {});
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }}
    >
      <Icon d={copied ? Icons.check : Icons.copy} size={12} color="currentColor" />
    </button>
  );
}
