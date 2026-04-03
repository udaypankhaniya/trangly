/**
 * dashboard.js — DashboardPage class for the projects view.
 * Uses SSE for real-time job status updates instead of polling.
 * Registered as Alpine.data component.
 *
 * Time helpers (timeAgo, duration) are loaded globally from utils.js.
 */

class DashboardPage {
  constructor() {
    // ── Navigation ─────────────────────────────────────────────
    this.mobileMenuOpen = false;

    // ── Data ────────────────────────────────────────────────────
    this.projects = [];
    this.activeJobs = [];

    // ── Footer stats ──────────────────────────────────────────
    this.stats = null;
    this._statsInterval = null;

    // ── SSE handles ────────────────────────────────────────────
    this._dashboardES = null;
    this._reconnectDelay = 1000;
    this._reconnectTimer = null;
    this._pollId = null; // fallback polling
    this._sseFailCount = 0;

    // ── Delete confirmation ─────────────────────────────────────
    this.showDeleteConfirm = false;
    this._pendingDelete = null;

    // ── Loading flag ──────────────────────────────────────────────
    this.loading = true;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();
    await this._loadAll();
    this._startDashboardStream();
    this.loadStats();
    this._statsInterval = setInterval(() => this.loadStats(), 30000);
  }

  destroy() {
    this._closeDashboardStream();
    if (this._pollId) clearInterval(this._pollId);
    if (this._reconnectTimer) clearTimeout(this._reconnectTimer);
    if (this._statsInterval) clearInterval(this._statsInterval);
  }

  // ── Data Loading ──────────────────────────────────────────────

  async _loadAll() {
    this.loading = true;
    try {
      await this.loadProjects();
    } finally {
      this.loading = false;
    }
  }

  async loadProjects() {
    const { ok, data } = await api.get('/api/projects');
    if (ok) this.projects = data.projects || [];
  }

  async loadStats() {
    const { ok, data } = await api.get('/api/system/stats');
    if (ok) this.stats = data;
  }

  // ── SSE Dashboard Stream ──────────────────────────────────────

  _startDashboardStream() {
    this._closeDashboardStream();
    if (this._pollId) { clearInterval(this._pollId); this._pollId = null; }

    const es = api.stream('/api/events');
    this._dashboardES = es;

    es.addEventListener('init', (e) => {
      try {
        const payload = JSON.parse(e.data);
        this.activeJobs = payload.jobs || [];
      } catch { /* ignore parse errors */ }
    });

    es.onmessage = (e) => {
      try {
        const evt = JSON.parse(e.data);
        if (evt.type === 'job_status') {
          this._applyJobStatusEvent(evt);
        }
      } catch { /* ignore */ }
    };

    es.onopen = () => {
      this._reconnectDelay = 1000;
      this._sseFailCount = 0;
    };

    es.onerror = () => {
      es.close();
      this._dashboardES = null;
      this._sseFailCount++;

      if (this._sseFailCount >= 3) {
        // Fall back to polling after 3 consecutive failures.
        if (!this._pollId) {
          this._pollId = setInterval(() => this._pollActiveJobs(), 5000);
          this._pollActiveJobs();
        }
        return;
      }

      // Reconnect with exponential backoff.
      this._reconnectTimer = setTimeout(() => {
        this._startDashboardStream();
      }, this._reconnectDelay);
      this._reconnectDelay = Math.min(this._reconnectDelay * 2, 30000);
    };
  }

  _closeDashboardStream() {
    if (this._dashboardES) {
      this._dashboardES.close();
      this._dashboardES = null;
    }
  }

  _applyJobStatusEvent(evt) {
    const idx = this.activeJobs.findIndex(j => j.id === evt.job_id);
    if (idx >= 0) {
      if (evt.status === 'success' || evt.status === 'failed') {
        // Terminal — remove from active and refresh projects (for last_deploy).
        this.activeJobs.splice(idx, 1);
        this.loadProjects();
      } else {
        this.activeJobs[idx] = { ...this.activeJobs[idx], status: evt.status, error: evt.error };
      }
    } else if (evt.status !== 'success' && evt.status !== 'failed') {
      // New active job — add to list.
      this.activeJobs.push({
        id: evt.job_id,
        project_id: evt.project_id,
        status: evt.status,
        commit_sha: evt.commit_sha,
        branch: evt.branch,
        error: evt.error,
      });
    } else {
      // Terminal event for a job we don't have — refresh projects.
      this.loadProjects();
    }

  }

  // Fallback polling (only used if SSE fails 3 times).
  async _pollActiveJobs() {
    const { ok, data } = await api.get('/api/queue');
    if (ok) {
      this.activeJobs = data.jobs || [];
    }
  }

  // ── Status Helpers ────────────────────────────────────────────

  jobForProject(projectId) {
    return this.activeJobs.find(j => j.project_id === projectId) || null;
  }

  projectNameForJob(j) {
    const p = this.projects.find(pr => pr.id === j.project_id);
    return p ? p.name : j.project_id?.substring(0, 8) || '—';
  }

  statusForProject(p) {
    const job = this.jobForProject(p.id);
    if (job) return job.status;
    return p.last_deploy?.status || 'idle';
  }

  statusColor(status) {
    const map = {
      idle:         'bg-gray-600/20 text-gray-400',
      success:      'bg-green-600/20 text-green-400',
      failed:       'bg-red-600/20 text-red-400',
      running:      'bg-blue-500/20 text-blue-400',
      building:     'bg-blue-500/20 text-blue-400',
      health_check: 'bg-blue-500/20 text-blue-400',
      swapping:     'bg-blue-500/20 text-blue-400',
      hot_swapping: 'bg-purple-500/20 text-purple-400',
      pending:      'bg-yellow-500/20 text-yellow-400',
      held:         'bg-yellow-500/20 text-yellow-400',
    };
    return map[status] || map.idle;
  }

  statusIcon(status) {
    const map = {
      idle:         'radio_button_unchecked',
      success:      'check_circle',
      failed:       'cancel',
      running:      'sync',
      building:     'build',
      health_check: 'monitor_heart',
      swapping:     'swap_horiz',
      hot_swapping: 'content_copy',
      pending:      'schedule',
      held:         'pause_circle',
    };
    return map[status] || map.idle;
  }

  // ── Last Deploy Helpers (Vercel-style) ────────────────────────

  lastDeploySummary(p) {
    const ld = p.last_deploy;
    if (!ld) return 'No deployments yet';
    const sha = ld.commit_sha?.substring(0, 7) || '?';
    const ago = timeAgo(ld.finished_at || ld.queued_at);
    const dur = duration(ld.started_at, ld.finished_at);
    let text = sha + ' on ' + ld.branch;
    if (ago) text += ' \u00b7 ' + ago;
    if (dur) text += ' \u00b7 ' + dur;
    return text;
  }

  lastDeployStatusIcon(p) {
    const ld = p.last_deploy;
    if (!ld) return 'radio_button_unchecked';
    if (ld.status === 'success') return 'check_circle';
    if (ld.status === 'failed') return 'cancel';
    return 'radio_button_unchecked';
  }

  // ── Project Form ──────────────────────────────────────────────

  /** Navigate to the project edit page. Pass null for a new project. */
  goToEdit(project) {
    window.location.href = project ? '/project/edit?id=' + project.id : '/project/edit';
  }

  deleteProject(project) {
    this._pendingDelete = project;
    this.showDeleteConfirm = true;
  }

  async confirmDelete() {
    const project = this._pendingDelete;
    if (!project) return;
    this.showDeleteConfirm = false;
    this._pendingDelete = null;
    const { ok, data } = await api.del('/api/projects/' + project.id);
    if (!ok) {
      api.toast(data?.error || 'Delete failed.', 'error');
      return;
    }
    api.toast('Project deleted', 'success');
    await this.loadProjects();
  }

  // ── Project Navigation ────────────────────────────────────────

  goToProject(p) {
    window.location.href = '/project?id=' + p.id;
  }

  // ── Time Helpers (exposed to templates) ───────────────────────

  timeAgo(d) { return timeAgo(d); }
  duration(s, e) { return duration(s, e); }

  // ── Manual Deploy ─────────────────────────────────────────────

  async triggerDeploy(project) {
    const { ok, data } = await api.post('/api/projects/' + project.id + '/deploy', {
      commit_sha: 'HEAD',
      branch: project.default_branch || 'main',
    });
    if (!ok) {
      api.toast(data?.error || 'Deploy failed.', 'error');
      return;
    }
    api.toast('Deploy queued', 'success');
  }
}

// Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('dashboardPage', () => new DashboardPage());
});
