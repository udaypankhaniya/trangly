/**
 * project_edit.js — ProjectEditPage class for /project/edit
 * Handles both "New Project" (no ?id param) and "Edit Project" (?id=<uuid>).
 * Registered as Alpine.data component.
 */

class ProjectEditPage {
  constructor() {
    // ── Navigation (required by sidebar + mobile_menu partials) ───
    this.mobileMenuOpen = false;

    // ── Mode ──────────────────────────────────────────────────────
    this.projectId = null;       // null = new project
    this.projectLoading = true;
    this.pageError = '';

    // ── Active Tab ────────────────────────────────────────────────
    this.activeTab = 'general'; // 'general' | 'env'

    // ── Form ──────────────────────────────────────────────────────
    this.form = this._emptyForm();
    this.formError = '';
    this.formLoading = false;

    // ── GitHub / repo dropdowns ───────────────────────────────────
    this.githubConnected = false;
    this.repos = [];
    this.branches = [];
    this.composeFiles = [];
    this.branchesLoading = false;
    this.composeLoading = false;
    this._tomSelect = null;

    // ── Environment Variables ─────────────────────────────────────
    // Each entry: { key: string, value: string, visible: boolean }
    this.envVars = [];
    this.envLoading = false;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();
    const params = new URLSearchParams(window.location.search);
    this.projectId = params.get('id') || null;

    await Promise.all([
      this._loadGitHubContext(),
      this.projectId ? this._loadProject() : Promise.resolve(),
      this.projectId ? this._loadEnvVars() : Promise.resolve(),
    ]);
    this.projectLoading = false;

    // Repo select is inside x-if; if visible immediately (repos pre-loaded)
    // we still call init, but the primary init path is the x-init on the element.
    if (!this.projectId) {
      this.$nextTick(() => this._initRepoSelect());
    }
  }

  destroy() {
    this._destroyRepoSelect();
  }

  // ── Data Loading ──────────────────────────────────────────────

  async _loadGitHubContext() {
    const { ok, data } = await api.get('/api/github/status');
    if (ok) {
      this.githubConnected = data.connected && data.installation_id > 0;
      if (this.githubConnected) {
        const r = await api.get('/api/github/repos');
        if (r.ok) this.repos = r.data.repositories || [];
      }
    }
  }

  async _loadProject() {
    const { ok, data } = await api.get('/api/projects/' + this.projectId);
    if (!ok) {
      this.pageError = data?.error || 'Failed to load project.';
      return;
    }
    const p = data;
    this.form = {
      name:           p.name,
      repo_full_name: p.repo_full_name,
      default_branch: p.default_branch || 'main',
      compose_path:   p.compose_path || 'docker-compose.yml',
      mem_limit_mb:   p.mem_limit_mb || 0,
      deploy_mode:    p.deploy_mode || 'rebuild',
      installation_id: p.installation_id || 0,
    };
    if (this.githubConnected) {
      this._fetchBranches(p.repo_full_name);
      this._fetchComposeFiles(p.repo_full_name, p.default_branch);
    }
  }

  async _loadEnvVars() {
    this.envLoading = true;
    const { ok, data } = await api.get('/api/projects/' + this.projectId + '/env');
    this.envLoading = false;
    if (ok) {
      this.envVars = (data.vars || []).map(v => ({ key: v.key, value: v.value, visible: false }));
    }
  }

  // ── Repo + Branch helpers ─────────────────────────────────────

  onRepoSelect() {
    const repo = this.repos.find(r => r.full_name === this.form.repo_full_name);
    if (repo) {
      this.form.name = this.form.name || repo.full_name.split('/')[1];
      this.form.default_branch = repo.default_branch || 'main';
      this._fetchBranches(repo.full_name);
      this._fetchComposeFiles(repo.full_name, repo.default_branch || 'main');
    }
  }

  async _fetchBranches(repoFullName) {
    this.branchesLoading = true;
    this.branches = [];
    try {
      const parts = repoFullName.split('/');
      const { ok, data } = await api.get('/api/github/repos/' + parts[0] + '/' + parts[1] + '/branches');
      if (ok && data.branches) this.branches = data.branches;
    } finally {
      this.branchesLoading = false;
    }
  }

  async _fetchComposeFiles(repoFullName, ref) {
    this.composeLoading = true;
    this.composeFiles = [];
    try {
      const parts = repoFullName.split('/');
      let url = '/api/github/repos/' + parts[0] + '/' + parts[1] + '/compose-files';
      if (ref) url += '?ref=' + encodeURIComponent(ref);
      const { ok, data } = await api.get(url);
      if (ok && data.files && data.files.length > 0) {
        this.composeFiles = data.files;
        if (!data.files.includes(this.form.compose_path)) {
          this.form.compose_path = data.files[0];
        }
      }
    } finally {
      this.composeLoading = false;
    }
  }

  onBranchChange() {
    if (this.form.repo_full_name && this.githubConnected) {
      this._fetchComposeFiles(this.form.repo_full_name, this.form.default_branch);
    }
  }

  _initRepoSelect() {
    this._destroyRepoSelect();
    const el = document.getElementById('repo-select');
    if (!el || typeof TomSelect === 'undefined') return;
    const self = this;
    this._tomSelect = new TomSelect(el, {
      options: this.repos.map(r => ({
        value: r.full_name,
        text: r.full_name,
        disabled: false,
      })),
      placeholder: 'Search repositories\u2026',
      sortField: { field: 'text', direction: 'asc' },
      onChange(value) {
        self.form.repo_full_name = value;
        self.onRepoSelect();
      },
    });
  }

  _destroyRepoSelect() {
    if (this._tomSelect) {
      this._tomSelect.destroy();
      this._tomSelect = null;
    }
  }

  // ── Environment Variable helpers ──────────────────────────────

  addVar() {
    this.envVars.push({ key: '', value: '', visible: false });
  }

  removeVar(index) {
    this.envVars.splice(index, 1);
  }

  toggleVisible(index) {
    this.envVars[index].visible = !this.envVars[index].visible;
  }

  /** Read a .env file client-side and populate envVars array (replaces existing). */
  parseEnvFile(file) {
    const reader = new FileReader();
    reader.onload = (e) => {
      const parsed = [];
      for (const line of e.target.result.split('\n')) {
        const t = line.trim();
        if (t === '' || t.startsWith('#')) continue;
        const idx = t.indexOf('=');
        if (idx === -1) continue;
        parsed.push({ key: t.slice(0, idx), value: t.slice(idx + 1), visible: false });
      }
      this.envVars = parsed;
    };
    reader.readAsText(file);
  }

  envVarCount() {
    return this.envVars.filter(v => v.key.trim() !== '').length;
  }

  /** Build a newline-delimited KEY=VALUE string from the envVars array. */
  _buildEnvString() {
    return this.envVars
      .filter(v => v.key.trim() !== '')
      .map(v => v.key.trim() + '=' + v.value)
      .join('\n');
  }

  // ── Save ──────────────────────────────────────────────────────

  async save() {
    this.formError = '';
    if (!this.form.name.trim()) {
      this.formError = 'Project name is required.';
      return;
    }
    if (!this.projectId && !this.form.repo_full_name.trim()) {
      this.formError = 'Repository is required.';
      return;
    }

    this.formLoading = true;
    try {
      const payload = {
        name:           this.form.name.trim(),
        default_branch: this.form.default_branch,
        compose_path:   this.form.compose_path,
        mem_limit_mb:   this.form.mem_limit_mb,
        deploy_mode:    this.form.deploy_mode,
        env_vars:       this._buildEnvString(),
      };

      let res;
      if (this.projectId) {
        res = await api.put('/api/projects/' + this.projectId, payload);
      } else {
        res = await api.post('/api/projects', {
          ...payload,
          repo_full_name:  this.form.repo_full_name.trim(),
          installation_id: this.form.installation_id || 0,
        });
      }

      if (!res.ok) {
        this.formError = res.data?.error || 'Failed to save project.';
        return;
      }
      api.toast(this.projectId ? 'Project updated' : 'Project created', 'success');
      window.location.href = '/dashboard';
    } finally {
      this.formLoading = false;
    }
  }

  cancel() {
    window.location.href = this.projectId ? '/project?id=' + this.projectId : '/dashboard';
  }

  // ── Helpers ───────────────────────────────────────────────────

  _emptyForm() {
    return {
      name:            '',
      repo_full_name:  '',
      default_branch:  'main',
      compose_path:    'docker-compose.yml',
      mem_limit_mb:    0,
      deploy_mode:     'rebuild',
      installation_id: 0,
    };
  }
}

document.addEventListener('alpine:init', () => {
  Alpine.data('projectEditPage', () => new ProjectEditPage());
});
