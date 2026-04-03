/**
 * profile.js — ProfilePage Alpine component for /profile.
 * Handles editing username/email and changing the password.
 */

class ProfilePage {
  constructor() {
    this.mobileMenuOpen = false;
    this.activeTab = 'info';

    // ── Profile Info ─────────────────────────────────────────────
    this.profile = { username: '', email: '' };
    this.profileSaving = false;
    this.profileErrors = {};
    this.profileSuccess = false;

    // ── Change Password ──────────────────────────────────────────
    this.pwForm = { current: '', new_: '', confirm: '' };
    this.pwLoading = false;
    this.pwErrors = {};
    this.pwSuccess = false;
    this.showCurrentPw = false;
    this.showNewPw = false;
    this.showConfirmPw = false;
  }

  // ── Lifecycle ─────────────────────────────────────────────────

  async init() {
    api.requireAuth();
    const params = new URLSearchParams(window.location.search);
    if (params.get('tab') === 'security') this.activeTab = 'security';
    await this.loadProfile();
  }

  // ── API Calls ─────────────────────────────────────────────────

  async loadProfile() {
    const { ok, data } = await api.get('/api/auth/me');
    if (ok) {
      this.profile.username = data.username || '';
      this.profile.email    = data.email    || '';
    }
  }

  async saveProfile() {
    this.profileErrors  = {};
    this.profileSuccess = false;

    if (!this.profile.username.trim()) {
      this.profileErrors.username = 'Username is required.';
      return;
    }

    this.profileSaving = true;
    try {
      const res = await api.put('/api/auth/profile', {
        username: this.profile.username.trim(),
        email:    this.profile.email.trim(),
      });
      if (res.status === 409) {
        this.profileErrors.username = 'That username is already taken.';
        return;
      }
      if (!res.ok) {
        this.profileErrors._global = res.data?.error || 'Failed to save profile.';
        return;
      }
      this.profileSuccess = true;
      api.toast('Profile updated', 'success');
      setTimeout(() => { this.profileSuccess = false; }, 3000);
    } finally {
      this.profileSaving = false;
    }
  }

  async changePassword() {
    this.pwErrors  = {};
    this.pwSuccess = false;

    if (!this.pwForm.current) {
      this.pwErrors.current = 'Current password is required.';
      return;
    }
    if (!this.pwForm.new_) {
      this.pwErrors.new_ = 'New password is required.';
      return;
    }
    if (this.pwForm.new_.length < 8) {
      this.pwErrors.new_ = 'New password must be at least 8 characters.';
      return;
    }
    if (this.pwForm.new_ !== this.pwForm.confirm) {
      this.pwErrors.confirm = 'Passwords do not match.';
      return;
    }

    this.pwLoading = true;
    try {
      const res = await api.post('/api/auth/change-password', {
        current_password: this.pwForm.current,
        new_password:     this.pwForm.new_,
      });
      if (res.status === 401) {
        this.pwErrors.current = 'Current password is incorrect.';
        return;
      }
      if (!res.ok) {
        this.pwErrors._global = res.data?.error || 'Failed to change password.';
        return;
      }
      this.pwForm    = { current: '', new_: '', confirm: '' };
      this.pwSuccess = true;
      api.toast('Password updated', 'success');
      setTimeout(() => { this.pwSuccess = false; }, 3000);
    } finally {
      this.pwLoading = false;
    }
  }
}

document.addEventListener('alpine:init', () => {
  Alpine.data('profilePage', () => new ProfilePage());
});
