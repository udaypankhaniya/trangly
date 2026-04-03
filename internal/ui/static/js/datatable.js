/**
 * datatable.js — Reusable server-side DataTable helper.
 *
 * Composable class that any Alpine.js component can instantiate to get
 * server-side sorting, searching, filtering, and pagination for free.
 *
 * Usage:
 *   this.table = new DataTable({
 *     fetchUrl:    '/api/deployments',
 *     defaultSort: 'queued_at',
 *     defaultDir:  'desc',
 *     pageSize:    20,
 *     // Map API response → { rows, total }
 *     parseResponse: (data) => ({ rows: data.deployments, total: data.total }),
 *   });
 *   await this.table.load();
 */

class DataTable {
  /**
   * @param {Object} opts
   * @param {string} opts.fetchUrl       - Base API endpoint (without query params)
   * @param {string} opts.defaultSort    - Default column to sort by
   * @param {string} opts.defaultDir     - Default direction: 'asc' | 'desc'
   * @param {number} opts.pageSize       - Initial page size
   * @param {Function} opts.parseResponse - (data) => { rows: [], total: number }
   */
  constructor(opts) {
    this.fetchUrl = opts.fetchUrl;
    this.sortBy = opts.defaultSort || 'queued_at';
    this.sortDir = opts.defaultDir || 'desc';
    this.pageSize = opts.pageSize || 20;
    this.parseResponse = opts.parseResponse || ((d) => ({ rows: d.deployments || [], total: d.total || 0 }));

    this.rows = [];
    this.total = 0;
    this.page = 1;
    this.search = '';
    this.loading = false;
    this.filters = {};

    this._debounceTimer = null;
  }

  /** Build the full URL with all query parameters. */
  _buildUrl() {
    const offset = (this.page - 1) * this.pageSize;
    let url = this.fetchUrl +
      '?limit=' + this.pageSize +
      '&offset=' + offset +
      '&sort_by=' + encodeURIComponent(this.sortBy) +
      '&sort_dir=' + encodeURIComponent(this.sortDir);

    if (this.search) {
      url += '&search=' + encodeURIComponent(this.search);
    }
    for (const [k, v] of Object.entries(this.filters)) {
      if (v) {
        url += '&' + encodeURIComponent(k) + '=' + encodeURIComponent(v);
      }
    }
    return url;
  }

  /** Fetch data from the server and update state. */
  async load() {
    this.loading = true;
    const url = this._buildUrl();
    const { ok, data } = await api.get(url);
    this.loading = false;
    if (!ok) return;
    const result = this.parseResponse(data);
    this.rows = result.rows;
    this.total = result.total;
  }

  /**
   * Toggle sort on a column. If same column, flip direction; otherwise sort desc.
   * @param {string} column
   */
  setSort(column) {
    if (this.sortBy === column) {
      this.sortDir = this.sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      this.sortBy = column;
      this.sortDir = 'desc';
    }
    this.page = 1;
    this.load();
  }

  /**
   * Set search query with debounce (300ms).
   * @param {string} query
   */
  setSearch(query) {
    if (this._debounceTimer) clearTimeout(this._debounceTimer);
    this._debounceTimer = setTimeout(() => {
      this.search = query;
      this.page = 1;
      this.load();
    }, 300);
  }

  /**
   * Set a filter value. Resets to page 1.
   * @param {string} key
   * @param {string} value
   */
  setFilter(key, value) {
    this.filters[key] = value;
    this.page = 1;
    this.load();
  }

  /**
   * Navigate to a specific page (1-based).
   * @param {number} n
   */
  setPage(n) {
    if (n < 1 || n > this.totalPages) return;
    this.page = n;
    this.load();
  }

  /** Go to next page. */
  nextPage() { this.setPage(this.page + 1); }

  /** Go to previous page. */
  prevPage() { this.setPage(this.page - 1); }

  /**
   * Change per-page size. Resets to page 1.
   * @param {number} n
   */
  setPageSize(n) {
    this.pageSize = n;
    this.page = 1;
    this.load();
  }

  /** Total number of pages. */
  get totalPages() {
    return Math.max(1, Math.ceil(this.total / this.pageSize));
  }

  /** Whether there is a next page. */
  get hasNext() { return this.page < this.totalPages; }

  /** Whether there is a previous page. */
  get hasPrev() { return this.page > 1; }

  /** 1-based index of the first row on the current page. */
  get showingFrom() {
    if (this.total === 0) return 0;
    return (this.page - 1) * this.pageSize + 1;
  }

  /** 1-based index of the last row on the current page. */
  get showingTo() {
    return Math.min(this.page * this.pageSize, this.total);
  }

  /**
   * Returns the sort indicator icon name for a column.
   * @param {string} column
   * @returns {string} Material Symbols icon name
   */
  sortIcon(column) {
    if (this.sortBy !== column) return 'unfold_more';
    return this.sortDir === 'asc' ? 'arrow_upward' : 'arrow_downward';
  }

  /**
   * Returns whether the given column is the active sort column.
   * @param {string} column
   * @returns {boolean}
   */
  isSorted(column) {
    return this.sortBy === column;
  }

  /**
   * Windowed page range for pagination controls.
   * Returns an array of page numbers and '...' separators.
   * Example: [1, '...', 4, 5, 6, '...', 10]
   * @returns {Array<number|string>}
   */
  get pageRange() {
    const total = this.totalPages;
    const current = this.page;
    const delta = 1; // pages around current

    if (total <= 7) {
      return Array.from({ length: total }, (_, i) => i + 1);
    }

    const range = [];
    const left = Math.max(2, current - delta);
    const right = Math.min(total - 1, current + delta);

    range.push(1);
    if (left > 2) range.push('...');
    for (let i = left; i <= right; i++) range.push(i);
    if (right < total - 1) range.push('...');
    range.push(total);

    return range;
  }

  /** Clean up debounce timer. */
  destroy() {
    if (this._debounceTimer) {
      clearTimeout(this._debounceTimer);
      this._debounceTimer = null;
    }
  }
}

window.DataTable = DataTable;
