/**
 * deployment.js — DeploymentPage class for viewing a single deployment's details and logs.
 * Similar to the project detail page but focused on one deployment.
 */

class DeploymentPage {
  constructor() {
    this.mobileMenuOpen = false;
    this.deployment = null;
    this.loadError = '';
    this.logLines = [];
    this._logSource = null;
    this.selectedPhase = null;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();
    const id = new URLSearchParams(window.location.search).get('id');
    if (!id) {
      this.loadError = 'No deployment ID provided.';
      return;
    }
    await this._loadDeployment(id);
  }

  destroy() {
    this._closeLogStream();
  }

  // ── Data Loading ──────────────────────────────────────────────

  async _loadDeployment(id) {
    const { ok, data } = await api.get('/api/deployments/' + id);
    if (!ok) {
      this.loadError = data?.error || 'Failed to load deployment.';
      return;
    }
    this.deployment = data;
    this._startLogStream(id);
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

  // ── Pipeline Phase Logic ──────────────────────────────────────

  get pipelinePhases() {
    if (this.deployment?.deploy_mode === 'hot_swap') {
      return ['fetch', 'hot_swap', 'cleanup'];
    }
    return ['fetch', 'build', 'health_check', 'swap', 'cleanup'];
  }

  phaseState(phase) {
    const d = this.deployment;
    if (!d) return 'pending';

    const isHotSwap = d.deploy_mode === 'hot_swap';
    const order = isHotSwap
      ? ['running', 'hot_swapping', 'success']
      : ['running', 'building', 'health_check', 'swapping', 'success'];

    const phaseMap = isHotSwap
      ? { fetch: 'running', hot_swap: 'hot_swapping', cleanup: 'success' }
      : { fetch: 'running', build: 'building', health_check: 'health_check', swap: 'swapping', cleanup: 'success' };

    if (d.status === 'success') return 'done';

    if (d.status === 'failed') {
      let failedStatus = 'running';
      const m = d.error?.match(/pipeline\[(\w+)]/);
      if (m) {
        const s = isHotSwap
          ? { fetch: 'running', hot_swap: 'hot_swapping' }
          : { fetch: 'running', build: 'building', health_check: 'health_check', swap: 'swapping' };
        failedStatus = s[m[1]] || 'running';
      }
      const failIdx = order.indexOf(failedStatus);
      const pidx = order.indexOf(phaseMap[phase]);
      if (pidx < failIdx) return 'done';
      if (pidx === failIdx) return 'active';
      return 'pending';
    }

    const cur = order.indexOf(d.status);
    const pidx = order.indexOf(phaseMap[phase]);
    if (cur < 0) return 'pending';
    if (pidx < cur) return 'done';
    if (pidx === cur) return 'active';
    return 'pending';
  }

  pipelineStepClass(phase) {
    const state = this.phaseState(phase);
    const isFailed = state === 'active' && this.deployment?.status === 'failed';
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

  // ── Time Helpers ──────────────────────────────────────────────

  timeAgo(d) { return timeAgo(d); }
  duration(s, e) { return duration(s, e); }
}

document.addEventListener('alpine:init', () => {
  Alpine.data('deploymentPage', () => new DeploymentPage());
});
