import { api, getToken, setToken } from './api.js';
import { connectRealtime } from './ws.js';
import { $, $$, escapeHtml, formatDate, formatBytes, statusLabel, statusClass, priorityClass, toast, avatarFor, humanDuration, evidenceIcon } from './ui.js';

const state = {
  user: null,
  page: 'tickets',
  ticketFilter: 'all',
  tickets: [],
  selectedTicket: null,
  realtime: null,
};

const loginView = $('#login-view');
const appView = $('#app-view');
const pageContent = $('#page-content');

boot();

async function boot() {
  bindGlobalEvents();
  if (!getToken()) { showLogin(); return; }
  try {
    const me = await api('/api/auth/me');
    state.user = me.user;
    showApp();
  } catch {
    setToken('');
    showLogin();
  }
}

function bindGlobalEvents() {
  $('#login-form').addEventListener('submit', onLogin);
  $('#logout-btn').addEventListener('click', logout);
  $('#refresh-btn').addEventListener('click', () => renderCurrent(true));
  document.addEventListener('click', (event) => {
    const nav = event.target.closest('[data-nav]');
    if (nav) { state.page = nav.dataset.nav; renderCurrent(); }
    const ticket = event.target.closest('[data-ticket-id]');
    if (ticket) openTicket(ticket.dataset.ticketId);
    const file = event.target.closest('[data-file-id]');
    if (file) openEvidence(file.dataset.fileId, file.dataset.fileName);
    const close = event.target.closest('[data-close-dialog]');
    if (close) $('#ticket-dialog').close();
  });
}

async function onLogin(event) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  try {
    const data = await api('/api/auth/login', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(Object.fromEntries(form.entries())) });
    setToken(data.token); state.user = data.user; showApp(); toast('Signed in successfully');
  } catch (error) { toast(error.message, 'error'); }
}

function logout() {
  state.realtime?.close(); state.realtime = null; setToken(''); state.user = null; showLogin();
}

function showLogin() {
  loginView.classList.remove('hidden'); appView.classList.add('hidden'); $('#login-form').reset();
}

function showApp() {
  loginView.classList.add('hidden'); appView.classList.remove('hidden');
  $('#user-name').textContent = state.user.fullName || state.user.email;
  $('#user-role').textContent = state.user.role;
  $('#user-avatar').textContent = avatarFor(state.user.fullName || state.user.email);
  buildNav(); renderCurrent(); startRealtime();
}

function buildNav() {
  const nav = $('#sidebar-nav');
  const items = state.user.role === 'support'
    ? [['tickets','Tickets'],['kpi','KPI Dashboard'],['users','User Registration']]
    : [['tickets','Open Ticket'],['history','My History']];
  nav.innerHTML = items.map(([key,label])=>`<button class="nav-btn ${state.page===key?'active':''}" data-nav="${key}">${label}</button>`).join('');
}

async function renderCurrent(force=false) {
  buildNav();
  const labels = {tickets: state.user.role==='support' ? 'Tickets' : 'Open Ticket', history:'My History', kpi:'KPI Dashboard', users:'User Registration'};
  $('#page-title').textContent = labels[state.page] || 'Tickets';
  $('#page-eyebrow').textContent = state.user.role === 'support' ? 'SUPPORT WORKSPACE' : 'SALESPERSON WORKSPACE';
  try {
    if (state.page === 'tickets') state.user.role === 'support' ? await renderSupportTickets(force) : renderSalesHome();
    if (state.page === 'history') await renderSalesHistory();
    if (state.page === 'kpi') await renderKPI();
    if (state.page === 'users') await renderUsers();
  } catch (error) {
    if (error.status === 401) logout(); else toast(error.message, 'error');
  }
}

function startRealtime() {
  state.realtime?.close();
  state.realtime = connectRealtime({
    onState: connected => {
      const el = $('#live-indicator'); el.classList.toggle('connected', connected); el.innerHTML = `<i></i> ${connected ? 'Live' : 'Reconnecting'}`;
    },
    onEvent: event => handleRealtimeEvent(event),
  });
}

async function handleRealtimeEvent(event) {
  if (event.type === 'ticket.created' || event.type === 'ticket.updated') {
    if (state.user.role === 'salesperson' && event.data?.createdBy !== state.user.id) return;
    const exists = state.tickets.some(t => t.id === event.id);
    if (exists) state.tickets = state.tickets.map(t => t.id===event.id ? event.data : t);
    else state.tickets.unshift(event.data);
    if (state.page === 'tickets' || state.page === 'history') renderCurrent();
    toast(`${event.data?.ticketCode || 'Ticket'} ${event.type === 'ticket.created' ? 'was created' : 'was updated'}`);
  }
}

async function renderSupportTickets() {
  const data = await api('/api/tickets?status=all'); state.tickets = data.tickets || [];
  pageContent.innerHTML = `
    <div class="hero">
      <div class="panel hero-copy"><div class="eyebrow">LIVE QUEUE</div><h2>Every ticket, one operational queue.</h2><p>New requests, evidence, status changes and resolution messages are delivered to this screen through WebSockets.</p></div>
      <div class="panel hero-stat">
        <div class="mini-stat"><strong>${state.tickets.length}</strong><span>Total tickets</span></div>
        <div class="mini-stat"><strong>${state.tickets.filter(t=>['open','in_progress'].includes(t.status)).length}</strong><span>Active queue</span></div>
        <div class="mini-stat"><strong>${state.tickets.filter(t=>t.priority==='urgent').length}</strong><span>Urgent priority</span></div>
      </div>
    </div>
    <div class="panel">
      <div class="panel-head"><div><h2>Tickets</h2><p>Click a ticket to open the full evidence folder and workflow controls.</p></div></div>
      <div class="filter-bar">
        ${['all','open','in_progress','resolved','rejected'].map(filter=>`<button class="filter-btn ${state.ticketFilter===filter?'active':''}" data-filter="${filter}">${statusLabel(filter)}</button>`).join('')}
      </div>
      <div id="ticket-queue" class="ticket-list"></div>
    </div>`;
  $$('.filter-btn').forEach(btn => btn.addEventListener('click', () => { state.ticketFilter = btn.dataset.filter; renderTicketQueue(); }));
  renderTicketQueue();
}

function renderTicketQueue() {
  const target = $('#ticket-queue');
  const items = state.ticketFilter === 'all' ? state.tickets : state.tickets.filter(t=>t.status===state.ticketFilter);
  if (!items.length) { target.innerHTML = '<div class="empty">No tickets in this view.</div>'; return; }
  target.innerHTML = items.map(ticketCard).join('');
}

function ticketCard(t) {
  return `<div class="ticket-row" data-ticket-id="${t.id}">
    <div class="ticket-main"><div class="ticket-title">${escapeHtml(t.title)}</div><div class="ticket-code">${escapeHtml(t.ticketCode)} · ${escapeHtml(t.creatorName || '')}</div>
    <div class="ticket-meta"><span class="${statusClass(t.status)}">${statusLabel(t.status)}</span><span class="${priorityClass(t.priority)}">${statusLabel(t.priority)}</span><span class="chip">${t.fileCount || 0} evidence</span></div></div>
    <div class="ticket-date">${formatDate(t.createdAt)}</div></div>`;
}

function renderSalesHome() {
  pageContent.innerHTML = `
    <div class="hero"><div class="panel hero-copy"><div class="eyebrow">NEW REQUEST</div><h2>Send the Support team a complete case.</h2><p>Attach photos, videos, audio and PDFs in the same ticket. Once submitted, Support receives it instantly without a page refresh.</p></div>
      <div class="panel hero-stat"><div class="mini-stat"><strong>${state.tickets.filter(t=>t.status==='open').length || '—'}</strong><span>Open cases in your history</span></div><div class="mini-stat"><strong>24/7</strong><span>Real-time delivery</span></div></div></div>
    <div class="panel"><div class="panel-head"><div><h2>Create ticket</h2><p>Required fields are kept intentionally small so the queue receives the case fast.</p></div></div>
      <form id="create-ticket-form" class="form-grid">
        <label>Title<input name="title" maxlength="160" placeholder="Example: Client received the wrong item" required></label>
        <label>Priority<select name="priority"><option value="normal">Normal</option><option value="low">Low</option><option value="high">High</option><option value="urgent">Urgent</option></select></label>
        <label class="form-full">Description<textarea name="description" maxlength="12000" placeholder="Describe what happened, what the customer needs and any relevant context." required></textarea></label>
        <label class="form-full">Evidence<input id="evidence-input" name="evidence" type="file" accept="image/*,audio/*,video/*,application/pdf" multiple><small class="muted">Images, audio, video and PDF. Multiple files are supported. Maximum follows the backend upload limit.</small></label>
        <div class="section-note form-full">The ticket becomes visible to Support immediately after the database transaction and evidence metadata are committed.</div>
        <div class="form-full"><button class="btn btn-primary" type="submit">Submit ticket</button></div>
      </form></div>`;
  $('#create-ticket-form').addEventListener('submit', createTicket);
}

async function createTicket(event) {
  event.preventDefault(); const form = event.currentTarget; const button = form.querySelector('button[type=submit]'); button.disabled=true; button.textContent='Submitting…';
  try {
    const response = await api('/api/tickets', { method:'POST', body:new FormData(form) });
    state.tickets.unshift(response); toast(`${response.ticketCode} created successfully`); form.reset();
  } catch(error) { toast(error.message,'error'); }
  finally { button.disabled=false; button.textContent='Submit ticket'; }
}

async function renderSalesHistory() {
  const data = await api('/api/tickets?mine=true'); state.tickets = data.tickets || [];
  pageContent.innerHTML = `<div class="panel"><div class="panel-head"><div><h2>My ticket history</h2><p>Read-only visibility. You can inspect evidence and the final Support resolution.</p></div><button class="btn btn-ghost" id="new-ticket-btn">New ticket</button></div><div id="history-list" class="ticket-list"></div></div>`;
  $('#new-ticket-btn').addEventListener('click', () => { state.page='tickets'; renderCurrent(); });
  const target = $('#history-list'); target.innerHTML = state.tickets.length ? state.tickets.map(ticketCard).join('') : '<div class="empty">You have not opened any tickets yet.</div>';
}

async function renderKPI() {
  const data = await api('/api/kpi');
  pageContent.innerHTML = `<div class="grid grid-4">
    <div class="panel kpi"><div class="kpi-label">Open</div><div class="kpi-value">${data.open}</div><div class="kpi-sub">Waiting for action</div></div>
    <div class="panel kpi"><div class="kpi-label">In progress</div><div class="kpi-value">${data.inProgress}</div><div class="kpi-sub">Currently being handled</div></div>
    <div class="panel kpi"><div class="kpi-label">Resolved today</div><div class="kpi-value">${data.resolvedToday}</div><div class="kpi-sub">Successful completions</div></div>
    <div class="panel kpi"><div class="kpi-label">Rejected today</div><div class="kpi-value">${data.rejectedToday}</div><div class="kpi-sub">Requires review / reason</div></div>
  </div><div class="panel" style="margin-top:16px"><div class="panel-head"><div><h2>Average resolution time</h2><p>Across all tickets that already have a resolution timestamp.</p></div></div><div class="kpi-value">${humanDuration(data.averageResolutionSeconds)}</div></div>`;
}

async function renderUsers() {
  const data = await api('/api/users');
  pageContent.innerHTML = `<div class="grid grid-2"><div class="panel"><div class="panel-head"><div><h2>Register user</h2><p>Support agents can create Salesperson or Support accounts.</p></div></div>
    <form id="user-form" class="form-grid"><label>Full name<input name="fullName" required maxlength="120"></label><label>Role<select name="role"><option value="salesperson">Salesperson</option><option value="support">Support</option></select></label><label>Email<input type="email" name="email" required></label><label>Password<input type="password" name="password" minlength="8" required></label><div class="form-full"><button class="btn btn-primary" type="submit">Create user</button></div></form></div>
    <div class="panel"><div class="panel-head"><div><h2>Registered users</h2><p>Active accounts currently known by the system.</p></div></div><table class="user-table"><thead><tr><th>Name</th><th>Role</th><th>Status</th></tr></thead><tbody>${data.users.map(u=>`<tr><td>${escapeHtml(u.fullName)}<br><span class="muted">${escapeHtml(u.email)}</span></td><td>${escapeHtml(u.role)}</td><td>${u.active ? 'Active' : 'Disabled'}</td></tr>`).join('')}</tbody></table></div></div>`;
  $('#user-form').addEventListener('submit', createUser);
}

async function createUser(event) {
  event.preventDefault(); const form=event.currentTarget;
  try { await api('/api/users',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(new FormData(form).entries()))}); toast('User created'); renderUsers(); }
  catch(error){toast(error.message,'error');}
}

async function openTicket(id) {
  try {
    const data = await api(`/api/tickets/${id}`); state.selectedTicket = data;
    $('#dialog-title').textContent = `${data.ticketCode} · ${data.title}`;
    $('#dialog-body').innerHTML = ticketDialog(data);
    $('#ticket-dialog').showModal();
    if (state.user.role==='support') bindSupportActions(data);
  } catch(error) { toast(error.message,'error'); }
}

function ticketDialog(t) {
  const evidence = (t.files||[]).length ? (t.files||[]).map(f=>`<div class="evidence-item"><div class="evidence-icon">${evidenceIcon(f.contentType)}</div><div class="evidence-info"><strong>${escapeHtml(f.name)}</strong><span>${escapeHtml(f.contentType)} · ${formatBytes(f.size)}</span></div><button class="btn btn-ghost" data-file-id="${f.id}" data-file-name="${escapeHtml(f.name)}">Open</button></div>`).join('') : '<div class="empty">No evidence was attached.</div>';
  const resolution = t.resolutionMessage ? `<div class="resolution-box ${t.status==='rejected'?'rejected':''}"><strong>${t.status==='rejected'?'Rejection reason':'Resolution message'}</strong><div class="detail-copy" style="margin-top:6px">${escapeHtml(t.resolutionMessage)}</div></div>` : '';
  const supportActions = state.user.role==='support' ? `<div style="margin-top:14px"><label>Resolution / rejection message<textarea id="resolution-message" placeholder="Required when resolving or rejecting">${escapeHtml(t.resolutionMessage || '')}</textarea></label><div class="status-actions"><button class="btn btn-ghost" data-status="open">Open</button><button class="btn btn-ghost" data-status="in_progress">In progress</button><button class="btn btn-primary" data-status="resolved">Resolve</button><button class="btn btn-danger" data-status="rejected">Reject</button></div></div>` : '';
  return `<div class="detail-grid"><div class="detail-block"><h3>Request</h3><div class="ticket-meta"><span class="${statusClass(t.status)}">${statusLabel(t.status)}</span><span class="${priorityClass(t.priority)}">${statusLabel(t.priority)}</span></div><div class="detail-copy" style="margin-top:14px">${escapeHtml(t.description)}</div>${resolution}${supportActions}</div>
    <div class="detail-block"><h3>Evidence folder</h3><div class="evidence-list">${evidence}</div></div></div><div class="detail-block" style="margin-top:16px"><h3>Timeline</h3><div class="ticket-meta"><span class="chip">Created ${formatDate(t.createdAt)}</span><span class="chip">Updated ${formatDate(t.updatedAt)}</span>${t.resolvedAt?`<span class="chip">Resolved ${formatDate(t.resolvedAt)}</span>`:''}</div><p class="muted" style="margin-bottom:0">Opened by ${escapeHtml(t.creatorName)} · ${escapeHtml(t.creatorEmail)}</p></div>`;
}

async function openEvidence(id, fileName) {
  try {
    const response = await fetch(`${(window.SUPPORT_API_BASE || '')}/api/files/${id}`, { headers: { Authorization: `Bearer ${getToken()}` } });
    if (!response.ok) throw new Error('Unable to open evidence');
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.target = '_blank';
    anchor.rel = 'noopener';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 30_000);
  } catch (error) {
    toast(error.message, 'error');
  }
}

function bindSupportActions(ticket) {
  $$('[data-status]', $('#dialog-body')).forEach(button=>button.addEventListener('click', async()=>{
    const next = button.dataset.status;
    let resolutionMessage = null;
    if (next==='resolved' || next==='rejected') {
      resolutionMessage = $('#resolution-message')?.value?.trim() || '';
      if (!resolutionMessage) { toast('A resolution or rejection message is mandatory.', 'error'); return; }
    }
    try { const updated = await api(`/api/tickets/${ticket.id}/status`, {method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({status:next,resolutionMessage})}); state.selectedTicket=updated; toast('Ticket updated'); $('#dialog-title').textContent=`${updated.ticketCode} · ${updated.title}`; $('#dialog-body').innerHTML=ticketDialog(updated); bindSupportActions(updated); renderCurrent(); }
    catch(error){toast(error.message,'error');}
  }));
}
