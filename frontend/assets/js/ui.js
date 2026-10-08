export const $ = (selector, root = document) => root.querySelector(selector);
export const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

export function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>'"]/g, char => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' }[char]));
}

export function formatDate(value) {
  if (!value) return '—';
  return new Intl.DateTimeFormat(undefined, { dateStyle:'medium', timeStyle:'short' }).format(new Date(value));
}
export function formatBytes(bytes) {
  if (!bytes) return '0 B';
  const units=['B','KB','MB','GB']; let n=bytes, i=0;
  while (n >= 1024 && i < units.length-1) { n/=1024; i++; }
  return `${n.toFixed(n >= 10 || i===0 ? 0 : 1)} ${units[i]}`;
}
export function statusLabel(value) { return value.replace('_',' '); }
export function statusClass(value) { return `chip ${value}`; }
export function priorityClass(value) { return `chip ${value}`; }
export function toast(message, type='success') {
  const root = $('#toast-root');
  const el = document.createElement('div'); el.className = `toast ${type}`; el.textContent = message;
  root.appendChild(el); setTimeout(() => el.remove(), 3400);
}
export function avatarFor(name='User') { return name.trim().split(/\s+/).slice(0,2).map(x=>x[0]).join('').toUpperCase(); }
export function humanDuration(seconds) {
  if (seconds == null) return '—';
  const s=Math.round(Number(seconds)); const d=Math.floor(s/86400); const h=Math.floor((s%86400)/3600); const m=Math.floor((s%3600)/60);
  if (d) return `${d}d ${h}h`; if (h) return `${h}h ${m}m`; return `${m}m`;
}
export function evidenceIcon(contentType='') {
  if (contentType.startsWith('image/')) return 'IMG';
  if (contentType.startsWith('video/')) return 'VID';
  if (contentType.startsWith('audio/')) return 'AUD';
  if (contentType === 'application/pdf') return 'PDF';
  return 'FILE';
}
