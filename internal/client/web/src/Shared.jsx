import React, { useState } from 'react';
import { Icon, Icons } from './Icons';
import { STATUS_COLORS } from './Constants';

export function Pill({ children, color }) {
  return (
    <span style={{ fontFamily: 'var(--mono)', fontSize: 10, fontWeight: 500, letterSpacing: '.04em', padding: '2px 6px', border: `1px solid ${color}44`, color, background: color + '14', textTransform: 'uppercase' }}>
      {children}
    </span>
  );
}

export function StatusDot({ status }) {
  const color = STATUS_COLORS[status] || '#6b7068';
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: color, boxShadow: status === 'online' ? `0 0 6px ${color}` : 'none', display: 'inline-block', flexShrink: 0, animation: status === 'online' ? 'pulse 2s ease infinite' : 'none' }} />
    </span>
  );
}

export function CopyBtn({ text }) {
  const [copied, setCopied] = useState(false);
  return (
    <button onClick={e => { e.stopPropagation(); navigator.clipboard?.writeText(text).catch(()=>{}); setCopied(true); setTimeout(() => setCopied(false), 1500); }}
      style={{ background: 'none', border: 'none', cursor: 'pointer', color: copied ? 'var(--accent)' : 'var(--text-dim)', padding: '2px 4px', display: 'flex', alignItems: 'center', transition: 'color .15s' }}>
      <Icon d={copied ? Icons.check : Icons.copy} size={12} color="currentColor" />
    </button>
  );
}

