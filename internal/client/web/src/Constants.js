export const STATUS_COLORS = { online: '#00e87a', offline: '#6b7068', error: '#ff4d4d' };
export const PROTO_COLORS = { https: '#4d9fff', tcp: '#f5c542', http: '#9b8fff', udp: '#10b981', static: '#c084fc' };
export function statusColor(s) { return s >= 500 ? '#ff4d4d' : s >= 400 ? '#f5c542' : s >= 200 ? '#00e87a' : '#9ba39c'; }
