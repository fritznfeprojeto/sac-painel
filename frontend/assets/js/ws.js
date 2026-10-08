import { API_BASE, getToken } from './api.js';

export function connectRealtime({ onEvent, onState }) {
  let socket = null;
  let timer = null;
  let closed = false;

  const open = () => {
    if (closed) return;
    const token = getToken();
    if (!token) return;
    const base = API_BASE || window.location.origin;
    const url = new URL(base.replace(/^http/, 'ws') + '/ws');
    const protocol = `ticket-auth.${token}`;
    socket = new WebSocket(url, [protocol]);
    socket.addEventListener('open', () => onState?.(true));
    socket.addEventListener('message', (event) => {
      try { onEvent?.(JSON.parse(event.data)); } catch {}
    });
    socket.addEventListener('close', () => {
      onState?.(false);
      if (!closed) {
        clearTimeout(timer);
        timer = setTimeout(open, 2500);
      }
    });
    socket.addEventListener('error', () => onState?.(false));
  };

  open();

  return {
    close() {
      closed = true;
      clearTimeout(timer);
      socket?.close();
      onState?.(false);
    },
  };
}
