/**
 * login.js — LoginPage class for the sign-in page.
 * Registered as Alpine.data component.
 */

class LoginPage {
  constructor() {
    this.username = '';
    this.password = '';
    this.loading = false;
    this.error = '';
    this.showPassword = false;
    this.errors = { username: '', password: '' };
  }

  clearError(field) {
    if (this.errors[field] !== undefined) this.errors[field] = '';
  }

  validateField(field) {
    if (!this[field] || !this[field].toString().trim()) {
      this.errors[field] = field.charAt(0).toUpperCase() + field.slice(1) + ' is required.';
    }
  }

  async init() {
    // Already authenticated → verify token is still valid before redirecting
    if (api.isAuthenticated()) {
      const { ok } = await api.get('/api/auth/me');
      if (ok) {
        window.location.replace('/dashboard');
        return;
      }
      // Token is stale — interceptor already cleared it, stay on login page
    }

    // Check if first-run setup is needed
    try {
      const { ok, data } = await api.get('/api/setup/status');
      if (ok && !data.setup_complete) {
        window.location.replace('/setup');
        return;
      }
    } catch (_) {
      // If status check fails, stay on login page.
    }

    // Redirected from a protected page
    const next = new URLSearchParams(window.location.search).get('next');
    if (next) {
      this.error = 'Please sign in to continue.';
    }
  }

  async doLogin() {
    this.error = '';
    this.loading = true;
    try {
      const { ok, data } = await api.post('/api/auth/login', {
        username: this.username,
        password: this.password,
      });
      if (!ok) {
        this.error = data?.error || 'Invalid username or password.';
        return;
      }
      api.setToken(data.token);
      const next = new URLSearchParams(window.location.search).get('next');
      window.location.replace(next || '/dashboard');
    } catch (_) {
      this.error = 'Network error \u2014 is the server running?';
    } finally {
      this.loading = false;
    }
  }
}

// Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('loginPage', () => new LoginPage());
});
