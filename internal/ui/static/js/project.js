/**
 * project.js — ProjectPage class for the project detail/deployment view.
 * Handles pipeline visualization, log streaming, and deployment history.
 * Registered as Alpine.data component.
 *
 * Time helpers (timeAgo, duration) are loaded globally from utils.js.
 */

class ProjectPage {
  constructor() {
    this.mobileMenuOpen = false;

    this.project = null;
    this.job = null;
    this.loadError = '';

    // ── Tabs ────────────────────────────────────────────────────
    this.tab = 'logs';

    // ── Logs ────────────────────────────────────────────────────
    this.logLines = [];
    this._logSource = null;

    // ── History ─────────────────────────────────────────────────
    this.history = [];
    this.historyLoading = false;
    this.historyHasMore = false;
    this._historyOffset = 0;

    // ── SSE for real-time status ────────────────────────────────
    this._dashboardES = null;
    this._reconnectDelay = 1000;
    this._reconnectTimer = null;
    this._sseFailCount = 0;
    this.selectedPhase = null;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();
    const id = new URLSearchParams(window.location.search).get('id');
    if (!id) {
      this.loadError = 'No project ID provided.';
      return;
    }
    await this._loadProject(id);
    if (this.project) {
      this._startDashboardStream();
    }
  }

  destroy() {
    this._closeLogStream();
    this._closeDashboardStream();
    if (this._reconnectTimer) clearTimeout(this._reconnectTimer);
  }

  // ── Data Loading ──────────────────────────────────────────────

  async _loadProject(id) {
    const { ok, data } = await api.get('/api/projects/' + id);
    if (!ok) {
      this.loadError = data?.error || 'Failed to load project.';
      return;
    }
    this.project = data;

    // Check for active job via queue
    const qRes = await api.get('/api/queue');
    if (qRes.ok) {
      const jobs = qRes.data.jobs || [];
      this.job = jobs.find(j => j.project_id === id) || null;
    }

    // If there's an active job, start log streaming
    if (this.job) {
      this._startLogStream(this.job.id);
    }
  }

  // ── Computed ──────────────────────────────────────────────────

  get currentStatus() {
    if (this.job) return this.job.status;
    return this.project?.last_deploy?.status || 'idle';
  }

  // ── SSE Dashboard Stream (real-time job status) ───────────────

  _startDashboardStream() {
    this._closeDashboardStream();
    const es = api.stream('/api/events');
    this._dashboardES = es;

    es.addEventListener('init', (e) => {
      try {
        const payload = JSON.parse(e.data);
        const jobs = payload.jobs || [];
        const myJob = jobs.find(j => j.project_id === this.project?.id);
        if (myJob) {
          this.job = myJob;
          if (!this._logSource) this._startLogStream(myJob.id);
        }
      } catch { /* ignore */ }
    });

    es.onmessage = (e) => {
      try {
        const evt = JSON.parse(e.data);
        if (evt.type === 'job_status' && evt.project_id === this.project?.id) {
          this._applyJobEvent(evt);
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
      if (this._sseFailCount >= 5) return;
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

  _applyJobEvent(evt) {
    if (evt.status === 'success' || evt.status === 'failed') {
      // Terminal — update job status, refresh project data for last_deploy
      this.job = this.job
        ? { ...this.job, status: evt.status, error: evt.error }
        : { id: evt.job_id, status: evt.status, error: evt.error, commit_sha: evt.commit_sha, branch: evt.branch };
      this._refreshProject();
    } else {
      // Active status update
      if (this.job) {
        this.job = { ...this.job, status: evt.status, error: evt.error };
      } else {
        this.job = {
          id: evt.job_id, project_id: evt.project_id,
          status: evt.status, commit_sha: evt.commit_sha,
          branch: evt.branch, error: evt.error,
        };
        this._startLogStream(evt.job_id);
      }
    }
  }

  async _refreshProject() {
    if (!this.project) return;
    const { ok, data } = await api.get('/api/projects/' + this.project.id);
    if (ok) {
      this.project = data;
    }
  }

  // ── Tabs ──────────────────────────────────────────────────────

  switchTab(tab) {
    this.tab = tab;
    if (tab === 'history' && this.history.length === 0) {
      this.loadHistory();
    }
  }

  // ── History ───────────────────────────────────────────────────

  async loadHistory(append) {
    if (!this.project) return;
    this.historyLoading = true;
    const offset = append ? this._historyOffset : 0;
    const { ok, data } = await api.get(
      '/api/deployments?project_id=' + this.project.id +
      '&limit=20&offset=' + offset
    );
    this.historyLoading = false;
    if (!ok) return;
    const jobs = data.deployments || [];
    if (append) {
      this.history = this.history.concat(jobs);
    } else {
      this.history = jobs;
    }
    this.historyHasMore = (offset + jobs.length) < (data.total || 0);
    this._historyOffset = offset + jobs.length;
  }

  async redeploy(historyJob) {
    const { ok, data } = await api.post('/api/projects/' + historyJob.project_id + '/deploy', {
      commit_sha: historyJob.commit_sha,
      branch: historyJob.branch,
    });
    if (!ok) {
      api.toast(data?.error || 'Redeploy failed.', 'error');
      return;
    }
    api.toast('Redeploy queued for ' + historyJob.commit_sha.substring(0, 7), 'success');
    this.tab = 'logs';
    this.job = { id: data.id, status: 'pending', commit_sha: historyJob.commit_sha, branch: historyJob.branch, project_id: historyJob.project_id };
    if (data.id) this._startLogStream(data.id);
  }

  // ── Log Streaming ─────────────────────────────────────────────

  _startLogStream(jobId) {
    this._closeLogStream();
    this.logLines = [];
    const es = api.stream('/api/deployments/' + jobId + '/logs');
    this._logSource = es;

    es.onmessage = (e) => {
      this.logLines.push(e.data);
      this.$nextTick(() => {
        const el = document.getElementById('log-output');
        if (el) el.scrollTop = el.scrollHeight;
      });
    };
    es.addEventListener('done', () => es.close());
    es.onerror = () => es.close();
  }

  _closeLogStream() {
    if (this._logSource) {
      this._logSource.close();
      this._logSource = null;
    }
  }

  // ── Pipeline Phase Styling ────────────────────────────────────

  get pipelinePhases() {
    if (this.project?.deploy_mode === 'hot_swap') {
      return ['fetch', 'hot_swap', 'cleanup'];
    }
    return ['fetch', 'build', 'health_check', 'swap', 'cleanup'];
  }

  phaseState(phase) {
    const job = this.job;
    if (!job) return 'pending';

    const isHotSwap = this.project?.deploy_mode === 'hot_swap';
    const order = isHotSwap
      ? ['running', 'hot_swapping', 'success']
      : ['running', 'building', 'health_check', 'swapping', 'success'];

    const phaseMap = isHotSwap
      ? { fetch: 'running', hot_swap: 'hot_swapping', cleanup: 'success' }
      : { fetch: 'running', build: 'building', health_check: 'health_check', swap: 'swapping', cleanup: 'success' };

    if (job.status === 'success') return 'done';

    if (job.status === 'failed') {
      let failedStatus = 'running';
      const m = job.error?.match(/pipeline\[(\w+)]/);
      if (m) {
        const s = isHotSwap
          ? { fetch:'running', hot_swap:'hot_swapping' }
          : { fetch:'running', build:'building', health_check:'health_check', swap:'swapping' };
        failedStatus = s[m[1]] || 'running';
      }
      const failIdx = order.indexOf(failedStatus);
      const pidx = order.indexOf(phaseMap[phase]);
      if (pidx < failIdx) return 'done';
      if (pidx === failIdx) return 'active';
      return 'pending';
    }

    const cur  = order.indexOf(job.status);
    const pidx = order.indexOf(phaseMap[phase]);
    if (cur < 0) return 'pending';
    if (pidx < cur)  return 'done';
    if (pidx === cur) return 'active';
    return 'pending';
  }

  pipelineStepClass(phase) {
    const state = this.phaseState(phase);
    const isFailed = state === 'active' && this.job?.status === 'failed';
    return {
      'ps-step--active':   state === 'active' && !isFailed,
      'ps-step--done':     state === 'done',
      'ps-step--pending':  state === 'pending',
      'ps-step--failed':   isFailed,
      'ps-step--selected': this.selectedPhase === phase,
    };
  }

  selectPhase(phase) {
    this.selectedPhase = phase;
    const el = document.getElementById('log-output');
    if (!el || this.logLines.length === 0) return;
    const keyword = phase.replace(/_/g, ' ').toLowerCase();
    const idx = this.logLines.findIndex(l =>
      l.toLowerCase().includes(keyword) || l.toLowerCase().includes(phase.toLowerCase())
    );
    if (idx < 0) return;
    const lineEls = el.querySelectorAll('.log-line');
    if (lineEls[idx]) lineEls[idx].scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  // ── Deploy ────────────────────────────────────────────────────

  async triggerDeploy() {
    if (!this.project) return;
    const { ok, data } = await api.post('/api/projects/' + this.project.id + '/deploy', {
      commit_sha: 'HEAD',
      branch: this.project.default_branch || 'main',
    });
    if (!ok) {
      api.toast(data?.error || 'Deploy failed.', 'error');
      return;
    }
    api.toast('Deploy queued', 'success');
    this.tab = 'logs';
    this.job = { id: data.id, status: 'pending', commit_sha: 'HEAD', branch: this.project.default_branch || 'main', project_id: this.project.id };
    if (data.id) this._startLogStream(data.id);
  }

  // ── Time Helpers (exposed to templates) ───────────────────────

  timeAgo(d) { return timeAgo(d); }
  duration(s, e) { return duration(s, e); }
}

// Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('projectPage', () => new ProjectPage());
});
