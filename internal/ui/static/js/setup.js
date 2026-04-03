/**
 * setup.js — SetupPage class for the first-run admin account creation.
 * Registered as Alpine.data component.
 */

class SetupPage {
  constructor() {
    this.username = '';
    this.email = '';
    this.password = '';
    this.confirmPassword = '';
    this.showPassword = false;
    this.loading = false;
    this.error = '';
    this.alreadySetup = false;
  }

  /** Password strength: 0 (empty), 1 (weak), 2 (fair), 3 (strong). */
  get passwordStrength() {
    const p = this.password;
    if (!p) return 0;
    let score = 0;
    if (p.length >= 8)  score++;
    if (p.length >= 12) score++;
    if (/[a-z]/.test(p) && /[A-Z]/.test(p)) score++;
    if (/\d/.test(p))   score++;
    if (/[^a-zA-Z0-9]/.test(p)) score++;
    if (score <= 1) return 1;
    if (score <= 3) return 2;
    return 3;
  }

  get strengthLabel() {
    return ['', 'Weak', 'Fair', 'Strong'][this.passwordStrength];
  }

  async init() {
    // If already authenticated, skip to dashboard.
    if (api.isAuthenticated()) {
      window.location.replace('/dashboard');
      return;
    }
    // Check if setup is already complete.
    try {
      const { ok, data } = await api.get('/api/setup/status');
      if (ok && data.setup_complete) {
        this.alreadySetup = true;
      }
    } catch (_) {
      // If the status check fails, show the form anyway.
    }
  }

  async doSetup() {
    this.error = '';

    // Client-side validation
    if (!this.username.trim()) {
      this.error = 'Username is required.';
      return;
    }
    if (!this.email.trim()) {
      this.error = 'Email is required.';
      return;
    }
    if (this.password.length < 8) {
      this.error = 'Password must be at least 8 characters.';
      return;
    }
    if (this.password !== this.confirmPassword) {
      this.error = 'Passwords do not match.';
      return;
    }

    this.loading = true;
    try {
      // 1. Create the admin account
      const { ok, data } = await api.post('/api/setup', {
        username: this.username.trim(),
        email: this.email.trim(),
        password: this.password,
      });
      if (!ok) {
        this.error = data?.error || 'Failed to create admin account.';
        return;
      }

      // 2. Auto-login with the new credentials
      const login = await api.post('/api/auth/login', {
        username: this.username.trim(),
        password: this.password,
      });
      if (!login.ok) {
        // Account was created but login failed — redirect to login page
        window.location.replace('/');
        return;
      }
      api.setToken(login.data.token);
      window.location.replace('/dashboard');
    } catch (_) {
      this.error = 'Network error \u2014 is the server running?';
    } finally {
      this.loading = false;
    }
  }
}

// Register with Alpine
document.addEventListener('alpine:init', () => {
  Alpine.data('setupPage', () => new SetupPage());
});
