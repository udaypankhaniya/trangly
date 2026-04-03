/**
 * utils.js — Shared utility functions used across all pages.
 * Loaded globally via base layout before page-specific scripts.
 */

/**
 * timeAgo returns a human-readable relative time string.
 * @param {string} dateStr - ISO 8601 date string
 * @returns {string} e.g. "just now", "5s ago", "3m ago", "2h ago", "yesterday", "5d ago"
 */
function timeAgo(dateStr) {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = Date.now();
  const sec = Math.floor((now - d.getTime()) / 1000);
  if (sec < 5) return 'just now';
  if (sec < 60) return sec + 's ago';
  const min = Math.floor(sec / 60);
  if (min < 60) return min + 'm ago';
  const hr = Math.floor(min / 60);
  if (hr < 24) return hr + 'h ago';
  const days = Math.floor(hr / 24);
  if (days === 1) return 'yesterday';
  if (days < 30) return days + 'd ago';
  return d.toLocaleDateString();
}

/**
 * duration returns a human-readable duration between two timestamps.
 * @param {string} startStr - ISO 8601 start time
 * @param {string} endStr   - ISO 8601 end time
 * @returns {string} e.g. "12s", "3m 45s"
 */
function duration(startStr, endStr) {
  if (!startStr || !endStr) return '';
  const ms = new Date(endStr) - new Date(startStr);
  if (ms < 0) return '';
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return sec + 's';
  const min = Math.floor(sec / 60);
  const rem = sec % 60;
  return min + 'm ' + rem + 's';
}

window.timeAgo = timeAgo;
window.duration = duration;
