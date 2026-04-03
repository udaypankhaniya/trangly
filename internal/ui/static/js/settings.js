/**
 * settings.js — SettingsPage class for system settings and GitHub integration.
 * Registered as Alpine.data component.
 */

class SettingsPage {
  constructor() {
    this.mobileMenuOpen = false;

    // ── GitHub state ────────────────────────────────────────────
    // Three states: 'not_connected' | 'app_created' | 'fully_connected'
    this.github = { connected: false, app_slug: '', installation_id: 0, has_installation: false };
    this.githubLoading = false;
    this.showDisconnectConfirm = false;
    this.removeFromGitHub = false;

    // ── System ──────────────────────────────────────────────────
    this.ram = null;
    this.preflight = [];

  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();

    // Check for redirect params from GitHub flow
    const params = new URLSearchParams(window.location.search);
    if (params.get('github') === 'connected') {
      api.toast('GitHub App created successfully!', 'success');
      history.replaceState(null, '', '/settings');
    }
    if (params.get('installed') === 'true') {
      api.toast('GitHub App installed on repositories!', 'success');
      history.replaceState(null, '', '/settings');
    }

    await Promise.all([
      this.loadGitHub(),
      this.loadRAM(),
      this.loadPreflight(),
    ]);
  }

  // ── Computed: GitHub State ────────────────────────────────────

  get githubState() {
    if (!this.github.connected) return 'not_connected';
    if (!this.github.has_installation) return 'app_created';
    return 'fully_connected';
  }

  // ── API Calls ─────────────────────────────────────────────────

  async loadGitHub() {
    const { ok, data } = await api.get('/api/github/status');
    if (ok) this.github = data;
  }

  async loadRAM() {
    const { ok, data } = await api.get('/api/system/ram');
    if (ok) this.ram = data;
  }

  async loadPreflight() {
    const { ok, data } = await api.get('/api/system/preflight');
    if (ok) this.preflight = data.checks || [];
  }

  // ── GitHub Connect (Step 1) ───────────────────────────────────

  async connectGitHub() {
    this.githubLoading = true;
    try {
      const { ok, data } = await api.get('/api/github/manifest');
      if (!ok || !data?.redirect_url || !data?.manifest) return;

      // GitHub App Manifest flow requires POSTing the manifest JSON as a form field.
      const form = document.createElement('form');
      form.method = 'POST';
      form.action = data.redirect_url;
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = 'manifest';
      input.value = JSON.stringify(data.manifest);
      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
    } finally {
      this.githubLoading = false;
    }
  }

  // ── GitHub Install Repos (Step 2) ─────────────────────────────

  async installOnRepos() {
    this.githubLoading = true;
    try {
      const { ok, data } = await api.get('/api/github/install-url');
      if (ok && data?.install_url) {
        window.location.href = data.install_url;
      }
    } finally {
      this.githubLoading = false;
    }
  }

  // ── GitHub Disconnect ─────────────────────────────────────────

  async disconnectGitHub() {
    this.githubLoading = true;
    try {
      const qs = this.removeFromGitHub ? '?remove_from_github=true' : '';
      const { ok } = await api.del('/api/github/app' + qs);
      if (ok) {
        this.showDisconnectConfirm = false;
        this.removeFromGitHub = false;
        this.github = { connected: false, app_slug: '', installation_id: 0, has_installation: false };
        api.toast('GitHub disconnected', 'success');
      } else {
        api.toast('Failed to disconnect', 'error');
      }
    } finally {
      this.githubLoading = false;
    }
  }

  // ── Helpers ───────────────────────────────────────────────────

  ramPercent() {
    if (!this.ram || !this.ram.total_mb) return 0;
    const used = this.ram.total_mb - this.ram.available_mb;
    return Math.round((used / this.ram.total_mb) * 100);
  }

  ramBarColor() {
    const p = this.ramPercent();
    if (p > 80) return 'var(--color-fail)';
    if (p > 60) return 'var(--color-warn)';
    return 'var(--color-ok)';
  }

  preflightIcon(status) {
    const map = {
      ok:   { icon: 'fa-circle-check',           color: 'var(--color-ok)' },
      warn: { icon: 'fa-triangle-exclamation',    color: 'var(--color-warn)' },
      fail: { icon: 'fa-circle-xmark',            color: 'var(--color-fail)' },
      skip: { icon: 'fa-circle-minus',            color: 'var(--text-muted)' },
    };
    const m = map[status] || map.skip;
    return 'fa-solid ' + m.icon + '" style="color:' + m.color;
  }

  preflightBadgeClass(status) {
    const map = {
      ok:   'badge-ok',
      warn: 'badge-warn',
      fail: 'badge-fail',
      skip: 'badge-idle',
    };
    return map[status] || map.skip;
  }
}

// Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('settingsPage', () => new SettingsPage());
});
