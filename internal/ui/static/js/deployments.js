/**
 * deployments.js — DeploymentsPage class for global deployment history.
 * Uses the reusable DataTable helper for server-side sorting, searching,
 * filtering, and pagination.
 */

class DeploymentsPage {
  constructor() {
    this.mobileMenuOpen = false;

    // Project lookup cache for name resolution.
    this.projects = [];
    this._projectMap = {};

    // DataTable instance for server-side operations.
    this.table = new DataTable({
      fetchUrl: '/api/deployments',
      defaultSort: 'queued_at',
      defaultDir: 'desc',
      pageSize: 20,
      parseResponse: (data) => ({
        rows: data.deployments || [],
        total: data.total || 0,
      }),
    });

    // Local search binding for the input (DataTable debounces internally).
    this._searchInput = '';
  }

  async init() {
    api.requireAuth();
    await this._loadProjects();
    await this.table.load();
  }

  destroy() {
    this.table.destroy();
  }

  async _loadProjects() {
    const { ok, data } = await api.get('/api/projects');
    if (ok) {
      this.projects = data.projects || [];
      this._projectMap = {};
      for (const p of this.projects) {
        this._projectMap[p.id] = p.name;
      }
    }
  }

  // --- Filter wrappers ---

  onFilterProject(value) {
    this.table.setFilter('project_id', value);
  }

  onFilterStatus(value) {
    this.table.setFilter('status', value);
  }

  onSearch(value) {
    this._searchInput = value;
    this.table.setSearch(value);
  }

  onPageSize(value) {
    this.table.setPageSize(parseInt(value, 10) || 20);
  }

  // --- Helpers ---

  projectName(projectId) {
    return this._projectMap[projectId] || projectId?.substring(0, 8) || '\u2014';
  }

  goToDeployment(d) {
    window.location.href = '/project?id=' + d.project_id;
  }

  timeAgo(d) { return timeAgo(d); }
  duration(s, e) { return duration(s, e); }
}

document.addEventListener('alpine:init', () => {
  Alpine.data('deploymentsPage', () => new DeploymentsPage());
});
