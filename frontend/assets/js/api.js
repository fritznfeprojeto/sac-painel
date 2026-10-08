const API_BASE = (window.SUPPORT_API_BASE || '').replace(/\/$/, '');

let token = sessionStorage.getItem('support_ticket_token') || '';

export function setToken(value) {
  token = value || '';
  if (token) sessionStorage.setItem('support_ticket_token', token);
  else sessionStorage.removeItem('support_ticket_token');
}
export function getToken() { return token; }

export async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (!headers.has('Accept')) headers.set('Accept', 'application/json');
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const response = await fetch(`${API_BASE}${path}`, { ...options, headers });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!response.ok) {
    const error = new Error(data?.error || `Request failed with ${response.status}`);
    error.status = response.status;
    throw error;
  }
  return data;
}

export { API_BASE };
