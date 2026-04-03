# Trangly — UI Architecture

This document describes the embedded frontend architecture. The UI is a **zero-build, zero-Node.js** frontend served directly from the Go binary via `embed.FS`.

---

## 1. Tech Stack

| Library | Version | Delivery | Purpose |
|---|---|---|---|
| Alpine.js | 3.14.1 | CDN (`unpkg.com`) | Reactive state management, `x-data` components |
| Tailwind CSS | Latest | CDN (`cdn.tailwindcss.com`) | Utility layout classes only — colors use CSS variables |
| Font Awesome | 6.5.1 | CDN (`cdnjs.cloudflare.com`) | Icons throughout the UI |
| Tom Select | 2.3.1 | CDN (`cdn.jsdelivr.net`) | Searchable repo dropdown on dashboard |
| Geist / Geist Mono | Latest | Google Fonts CDN | Typography |

**Hard rule:** No Node.js, no npm, no webpack, no Vite. Every page is a self-contained HTML file with `<script>` tags.

---

## 2. File Structure

```
internal/ui/
├── ui.go                      ← embed.FS + Fiber route registration
└── static/
    ├── index.html             ← Login page
    ├── setup.html             ← First-run admin creation
    ├── dashboard.html         ← Main project/deploy view
    ├── settings.html          ← GitHub integration + system info
    ├── style.css              ← CSS custom properties + component classes
    └── js/
        ├── api.js             ← ApiClient singleton (window.api)
        ├── tailwind-setup.js  ← Tailwind config (loaded after CDN)
        ├── login.js           ← LoginPage Alpine component
        ├── setup.js           ← SetupPage Alpine component
        ├── dashboard.js       ← DashboardPage Alpine component
        └── settings.js        ← SettingsPage Alpine component
```

---

## 3. Embedding & Routing (`ui.go`)

All files under `internal/ui/static/` are embedded into the binary at compile time via Go's `//go:embed static` directive.

### Route Table

| Path | Auth Required | Serves | Behavior |
|---|---|---|---|
| `GET /` | No | `index.html` | Login page |
| `GET /login` | No | `index.html` | Alias for `/` |
| `GET /setup` | No | `setup.html` | Redirects to `/dashboard` if `sy_auth` cookie present |
| `GET /dashboard` | Yes (cookie) | `dashboard.html` | Redirects to `/?next=/dashboard` without cookie |
| `GET /settings` | Yes (cookie) | `settings.html` | Redirects to `/?next=/settings` without cookie |
| `GET /static/*` | No | Static assets | Fiber filesystem middleware |
| `*` (catch-all) | — | 302 redirect | Redirects to `/dashboard` |

### Auth Guard (Cookie-Based)

The UI layer checks only for the **presence** of the `sy_auth` cookie — not its cryptographic validity. Real JWT validation happens in the API middleware layer. This keeps the UI routing fast and simple while preventing unauthenticated users from seeing protected page shells.

```
Browser → GET /dashboard
  ├── sy_auth cookie present? → serve dashboard.html
  └── no cookie? → 302 to /?next=/dashboard
```

---

## 4. API Client (`api.js`)

`window.api` is a singleton `ApiClient` instance available to all pages. Every API call goes through it.

### Token Management

- **Storage:** JWT stored in `localStorage` (`sy_token` key) **and** as a browser cookie (`sy_auth`).
- **Dual storage rationale:** `localStorage` is read by JS for API `Authorization` headers. The cookie is read by `ui.go` for page-level auth gating (server-side, before JS loads).
- **Login:** `api.setToken(token)` writes both. Cookie expires in 7 days.
- **Logout:** `api.clearToken()` removes both. `api.logout()` clears and redirects to `/`.

### Request Flow

```
api.get('/api/projects')
  → fetch() with Authorization: Bearer {token}
  → 401/403? → clearToken() + redirect to / (except /api/auth/login)
  → parse JSON if Content-Type matches
  → return { ok, status, data }
```

### SSE Streaming

```js
const es = api.stream('/api/events');
// Token passed as query parameter: /api/events?token=...
// Returns native EventSource — caller manages listeners
```

### Toast Notifications

```js
api.toast('Project created', 'success');   // types: success | error | warning | info
```

Toasts render in a fixed top-right container (`#sy-toasts`), auto-dismiss after 3.5 seconds with fade-out animation. HTML is escaped via `textContent` to prevent XSS.

### Utilities

- `api.timeAgo(dateStr)` — human-readable relative time ("5m ago", "2d ago")
- `api.isAuthenticated()` — checks `localStorage` for token
- `api.requireAuth()` — redirects to `/` if not authenticated

---

## 5. Pages

### 5.1 Login Page (`index.html` + `login.js`)

**Alpine component:** `loginPage()`

**Init flow:**
1. If `api.isAuthenticated()` → redirect to `/dashboard`
2. Fetch `GET /api/setup/status` → if `setup_complete === false` → redirect to `/setup`
3. Check `?next=` query param → show "Please sign in to continue" message

**Login flow:**
1. `POST /api/auth/login` with `{ username, password }`
2. On success → `api.setToken(data.token)` → redirect to `?next` param or `/dashboard`
3. On failure → show inline error

### 5.2 Setup Page (`setup.html` + `setup.js`)

**Alpine component:** `setupPage()`

**Purpose:** First-run admin account creation. Only works when no admin exists.

**Init flow:**
1. If `api.isAuthenticated()` → redirect to `/dashboard`
2. Fetch `GET /api/setup/status` → if `setup_complete === true` → show "already configured" banner with link to sign-in

**Setup flow:**
1. Client-side validation: username required, email required, password ≥ 8 chars, passwords match
2. `POST /api/setup` with `{ username, email, password }`
3. On success → auto-login via `POST /api/auth/login` → redirect to `/dashboard`
4. If setup rejected (409) → show error

**Security:** `POST /api/setup` is a one-shot endpoint. It returns `409 Conflict` once any admin exists, making it safe to leave as a public route.

### 5.3 Dashboard Page (`dashboard.html` + `dashboard.js`)

**Alpine component:** `dashboardPage()`

This is the main page — projects grid, active deploys, log streaming, deploy history.

#### State

```js
this.projects = [];          // From GET /api/projects
this.activeJobs = [];        // Real-time via SSE
this.repos = [];             // From GET /api/github/repos
this.githubConnected = false;
this.modal = null;           // Project detail/log modal
this.showForm = false;       // Add/edit project form
```

#### Real-Time Updates (SSE)

The dashboard maintains a persistent SSE connection to `GET /api/events`:

- **`init` event:** Receives the full list of active jobs on connect.
- **`message` events:** Receives `job_status` events with `{ job_id, project_id, status, commit_sha, branch, error }`.
- **State reconciliation:** Terminal statuses (`success`/`failed`) remove jobs from `activeJobs` and trigger a project refresh. Non-terminal statuses update in-place or add new jobs.
- **Reconnect:** On SSE error, exponential backoff (1s → 2s → 4s → ... max 30s). After 3 consecutive failures, falls back to polling `GET /api/queue` every 5 seconds.

#### Project Cards

Each project renders as a card showing:
- Status badge (color-coded by current deploy state)
- Repository link + branch + memory limit
- Last deploy summary (Vercel-style: `abc1234 on main · 5m ago · 23s`)
- Action buttons: Deploy, Logs, Edit, Delete

#### Add/Edit Project Modal

- **Repo picker:** If GitHub is connected, uses Tom Select searchable dropdown populated from `GET /api/github/repos`. Otherwise, shows a plain text input.
- **Fields:** Name, repository, branch, compose path, memory limit
- **Auto-fill:** Selecting a repo auto-sets the project name and default branch
- Saves via `POST /api/projects` (create) or `PUT /api/projects/:id` (update)

#### Log / Detail Modal

Two tabs:

**Logs Tab:**
- Pipeline progress bar showing 5 phases: `fetch → build → health_check → swap → cleanup`
- Each phase shows active/complete/pending state based on current job status
- Real-time log output via SSE stream to `GET /api/deployments/:id/logs`
- Auto-scrolls to bottom as new lines arrive
- Log stream ends when server sends a `done` event

**History Tab:**
- Paginated deployment table (`GET /api/deployments?project_id=...&limit=20&offset=0`)
- Each row: commit SHA (GitHub link), branch, status badge, duration, timestamp
- "Redeploy" button per entry → `POST /api/projects/:id/deploy` with specific SHA
- Load-more pagination

### 5.4 Settings Page (`settings.html` + `settings.js`)

**Alpine component:** `settingsPage()`

Three sections:

#### GitHub Integration

State machine with three states driven by `GET /api/github/status`:

| State | Condition | UI |
|---|---|---|
| `not_connected` | `connected === false` | "Connect GitHub" button |
| `app_created` | `connected && !has_installation` | App slug shown + "Select Repositories" button |
| `fully_connected` | `connected && has_installation` | App slug + installation ID + "Manage on GitHub" link |

**Connect flow:** `GET /api/github/manifest` → builds a hidden `<form>` → `POST` to GitHub's manifest URL → GitHub redirects back to `/api/github/callback` → redirects to `/settings?github=connected`.

**Disconnect:** Confirmation modal with optional "also remove from GitHub" checkbox → `DELETE /api/github/app`.

#### System Memory

- Fetches `GET /api/system/ram` → shows progress bar with color coding (green < 60%, yellow < 80%, red ≥ 80%)
- Text: `X MB used of Y MB (Z%)`

#### Preflight Checks

- Fetches `GET /api/system/preflight` → renders structured `CheckResult` objects as a table
- Columns: status icon, check name, status badge, detail text, fix suggestion
- Spinner while loading

---

## 6. Theme System

Trangly supports light and dark themes via CSS custom properties.

### How It Works

1. **Inline script** in every `<head>` reads `localStorage.sy_theme` and sets `data-theme` attribute on `<html>` **before** the page renders (prevents flash of wrong theme)
2. **CSS** defines two sets of custom properties in `style.css`:
   ```css
   :root, [data-theme="dark"] { --bg-page: #0d1117; ... }
   [data-theme="light"] { --bg-page: #ffffff; ... }
   ```
3. **Toggle button** (in navbar) calls `initTheme()` from `api.js` which swaps the attribute and persists to `localStorage`

### Color Architecture

All colors are defined as CSS custom properties — Tailwind is used **only for layout utilities** (flex, grid, padding, margin, etc.), never for colors. This is intentional:

- Colors change with the theme → CSS variables handle this
- Layout doesn't change with theme → Tailwind classes are fine
- Component classes (`.btn-primary`, `.badge-ok`, `.alert-error`) reference CSS variables

```css
/* Component example — colors from CSS vars, not Tailwind */
.btn-primary {
  background: var(--accent);
  color: var(--accent-fg);
}
```

### Tailwind Config (`tailwind-setup.js`)

Loaded after the Tailwind CDN script. Extends with:
- GitHub dark-mode color palette (for reference, not primary usage)
- Mono font stack (SF Mono, Fira Code, Consolas)
- Custom animations: `fade-in`, `slide-in`, `pulse-dot`

---

## 7. SSE Architecture

Trangly uses Server-Sent Events (not WebSockets) for all real-time communication.

### Two SSE Endpoints

| Endpoint | Purpose | Used By |
|---|---|---|
| `GET /api/events` | Dashboard-wide job status updates | `dashboard.js` |
| `GET /api/deployments/:id/logs` | Per-job log streaming | Log modal in `dashboard.js` |

### Auth for SSE

SSE uses `EventSource` which doesn't support custom headers. The JWT token is passed as a query parameter: `?token=...`. The server validates this token the same way it validates the `Authorization` header.

### Dashboard Events Stream

```
Client: EventSource('/api/events?token=...')

Server → event: init
         data: { "jobs": [...activeJobs] }

Server → data: { "type": "job_status", "job_id": "...", "status": "building", ... }
Server → data: { "type": "job_status", "job_id": "...", "status": "success", ... }
```

### Log Stream

```
Client: EventSource('/api/deployments/{id}/logs?token=...')

Server → data: [2026-04-01 12:00:01] Cloning repository...
Server → data: [2026-04-01 12:00:03] Running docker compose build...
Server → event: done
         data: (empty)
```

The `done` event signals the log stream is complete. The client closes the `EventSource` on receiving it.

### Reconnection Strategy

Dashboard SSE reconnects with exponential backoff: 1s → 2s → 4s → 8s → ... → 30s max. After 3 consecutive failures, it falls back to polling `GET /api/queue` every 5 seconds. The fallback is transparent to the user — the UI continues to update.

---

## 8. Component Registration Pattern

Each page uses the same pattern for Alpine.js component registration:

```js
// 1. Define a class with constructor (state), init (lifecycle), and methods
class DashboardPage {
  constructor() { /* reactive state */ }
  async init() { /* runs after Alpine mounts */ }
  destroy() { /* cleanup: close SSE, clear timers */ }
  // ... methods
}

// 2. Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('dashboardPage', () => new DashboardPage());
});

// 3. Reference in HTML
// <div x-data="dashboardPage()" x-cloak>
```

### Page Script Loading Order

Every HTML page loads scripts in this order (at the end of `<body>`):
1. `api.js` — must load first (provides `window.api`)
2. Page-specific JS (e.g., `dashboard.js`)

CDN scripts load in `<head>` with `defer` (Alpine.js) or synchronously (Tailwind, Font Awesome).

---

## 9. Modal & Form Patterns

### Modal Pattern

Modals use Alpine `x-show` with backdrop blur:

```html
<div x-show="showForm" x-cloak
     class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
     @click.self="showForm = false">
  <div class="modal-card" @click.stop>
    <!-- content -->
  </div>
</div>
```

- `@click.self` closes on backdrop click
- `@click.stop` prevents closing when clicking inside the modal
- `x-cloak` hides until Alpine initializes (prevents flash)

### Form Validation Pattern

Client-side validation runs before API calls. Errors displayed inline:

```html
<div x-show="formError" x-cloak class="alert-error">
  <span x-text="formError"></span>
</div>
```

Server-side errors from `{ "error": "..." }` responses are shown in the same error zone.

---

## 10. Key CSS Classes (from `style.css`)

These component classes are the UI building blocks:

| Class | Purpose |
|---|---|
| `.btn-primary` | Green accent button |
| `.btn-secondary` | Subtle outline button |
| `.btn-danger` | Red destructive action button |
| `.badge-ok` / `.badge-fail` / `.badge-warn` / `.badge-idle` | Status indicators |
| `.alert-error` | Inline error message box |
| `.toast-success` / `.toast-error` / `.toast-warn` / `.toast-info` | Toast notification variants |
| `.field-group` | Form field wrapper (label + input + error) |
| `.input-wrapper` | Input with icon prefix |
| `.login-card` / `.modal-card` | Card containers |
| `.pipeline-bar` | 5-phase deploy progress visualization |

All classes use CSS custom properties for colors so they automatically adapt to theme changes.
