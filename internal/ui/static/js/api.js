/**
 * api.js — Trangly API client.
 * Class-based with interceptors, toast system, and token management.
 * Exports: window.api (singleton instance of ApiClient)
 */

class ApiClient {
  constructor() {
    this.TOKEN_KEY = 'sy_token';
    this.AUTH_COOKIE = 'sy_auth';
    this._toastContainer = null;
  }

  // ── Token Management ────────────────────────────────────────────

  getToken() {
    return localStorage.getItem(this.TOKEN_KEY) || '';
  }

  setToken(token) {
    localStorage.setItem(this.TOKEN_KEY, token);
    const exp = new Date(Date.now() + 7 * 86400 * 1000).toUTCString();
    // Add Secure flag only when served over HTTPS so the cookie is not transmitted
    // over plain HTTP (v1.0 may run without TLS on a LAN / localhost).
    const sec = location.protocol === 'https:' ? '; Secure' : '';
    document.cookie = this.AUTH_COOKIE + '=' + encodeURIComponent(token) +
      '; path=/; expires=' + exp + '; SameSite=Lax' + sec;
  }

  clearToken() {
    localStorage.removeItem(this.TOKEN_KEY);
    const sec = location.protocol === 'https:' ? '; Secure' : '';
    document.cookie = this.AUTH_COOKIE +
      '=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT; SameSite=Lax' + sec;
  }

  isAuthenticated() {
    return !!this.getToken();
  }

  requireAuth() {
    if (!this.isAuthenticated()) {
      window.location.replace('/');
    }
  }

  logout() {
    this.clearToken();
    window.location.replace('/');
  }

  // ── Toast Notifications ─────────────────────────────────────────

  _ensureToastContainer() {
    if (this._toastContainer) return;
    this._toastContainer = document.createElement('div');
    this._toastContainer.id = 'sy-toasts';
    this._toastContainer.className =
      'fixed top-4 right-4 z-[200] flex flex-col gap-2 pointer-events-none';
    document.body.appendChild(this._toastContainer);
  }

  toast(message, type = 'info', duration = 3500) {
    this._ensureToastContainer();

    const cfg = {
      success: { cls: 'toast-success', icon: 'fa-circle-check' },
      error:   { cls: 'toast-error',   icon: 'fa-circle-xmark' },
      warning: { cls: 'toast-warn',    icon: 'fa-triangle-exclamation' },
      info:    { cls: 'toast-info',    icon: 'fa-circle-info' },
    };
    const c = cfg[type] || cfg.info;

    const el = document.createElement('div');
    el.className = 'toast animate-toast ' + c.cls;
    el.innerHTML =
      '<i class="fa-solid ' + c.icon + '"></i>' +
      '<span>' + this._escapeHtml(message) + '</span>';
    this._toastContainer.appendChild(el);

    setTimeout(() => {
      el.style.opacity = '0';
      el.style.transform = 'translateX(12px)';
      el.style.transition = 'opacity 300ms, transform 300ms';
      setTimeout(() => el.remove(), 300);
    }, duration);
  }

  _escapeHtml(str) {
    const d = document.createElement('div');
    d.textContent = str;
    return d.innerHTML;
  }

  // ── Core Request (with interceptors) ────────────────────────────

  async request(method, path, body) {
    const token = this.getToken();
    const opts = {
      method,
      headers: { 'Content-Type': 'application/json' },
    };
    if (token) opts.headers['Authorization'] = 'Bearer ' + token;
    if (body !== undefined) opts.body = JSON.stringify(body);

    let res;
    try {
      res = await fetch(path, opts);
    } catch (e) {
      this.toast('Network error \u2014 is the server running?', 'error');
      return { ok: false, status: 0, data: null };
    }

    // Interceptor: 401/403 → force logout (skip for login and change-password endpoints)
    const skipLogout = ['/api/auth/login', '/api/auth/change-password'];
    if ((res.status === 401 || res.status === 403) && !skipLogout.includes(path)) {
      this.clearToken();
      window.location.replace('/');
      return { ok: false, status: res.status, data: null };
    }

    let data = null;
    const ct = res.headers.get('Content-Type') || '';
    if (ct.includes('application/json')) {
      data = await res.json();
    }
    return { ok: res.ok, status: res.status, data };
  }

  // ── Convenience Methods ─────────────────────────────────────────

  get(path)        { return this.request('GET', path); }
  post(path, body) { return this.request('POST', path, body); }
  put(path, body)  { return this.request('PUT', path, body); }
  del(path)        { return this.request('DELETE', path); }

  // ── SSE Stream ──────────────────────────────────────────────────

  stream(path) {
    const token = this.getToken();
    const sep = path.includes('?') ? '&' : '?';
    return new EventSource(path + sep + 'token=' + encodeURIComponent(token));
  }

  // ── Utilities ───────────────────────────────────────────────────

  timeAgo(dateStr) {
    if (!dateStr) return '';
    const diff = (Date.now() - new Date(dateStr).getTime()) / 1000;
    if (diff < 60)    return Math.floor(diff) + 's ago';
    if (diff < 3600)  return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    return Math.floor(diff / 86400) + 'd ago';
  }
}

// Singleton
window.api = new ApiClient();

// ── Theme Toggle ────────────────────────────────────────────────────

function applyTheme(theme) {
  const html = document.documentElement;
  const icon  = document.getElementById('theme-icon');
  const label = document.getElementById('theme-label');
  html.setAttribute('data-theme', theme);
  html.classList.toggle('dark', theme === 'dark');
  localStorage.setItem('sy_theme', theme);
  if (icon) {
    // Font Awesome: show sun in dark (→ switch to light), moon in light (→ switch to dark)
    icon.className = theme === 'dark' ? 'fa-solid fa-sun' : 'fa-solid fa-moon';
  }
  if (label) {
    label.textContent = theme === 'dark' ? 'Light mode' : 'Dark mode';
  }
}

window.applyTheme = applyTheme;

function initTheme() {
  applyTheme(localStorage.getItem('sy_theme') || 'dark');

  const btn = document.getElementById('theme-toggle');
  if (btn) {
    btn.addEventListener('click', () => {
      const current = document.documentElement.getAttribute('data-theme');
      applyTheme(current === 'dark' ? 'light' : 'dark');
    });
  }
}

document.addEventListener('DOMContentLoaded', initTheme);
