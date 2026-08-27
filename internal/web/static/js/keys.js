/**
 * SSHPilot SSH Keys Vault Manager
 * Security & Microinteraction Enhanced
 */

const SSHPilotKeys = {
  keys: [],
  isLoading: false,

  init() {
    this.container = document.getElementById('keys-list-container');
    this.genBtn = document.getElementById('btn-gen-key');

    if (this.genBtn) {
      this.genBtn.addEventListener('click', () => this.handleGenerateKey());
    }

    this.loadKeys();
  },

  async loadKeys() {
    this.isLoading = true;
    this.renderSkeleton();

    try {
      const res = await fetch('/api/keys');
      const data = await res.json();
      this.keys = data.keys || [];
      this.isLoading = false;
      this.render();
    } catch (e) {
      this.isLoading = false;
      console.error('Failed to load keys', e);
      window.SSHPilotApp.showToast('Failed to load cryptographic keys from vault', 'danger');
    }
  },

  renderSkeleton() {
    if (!this.container) return;
    this.container.innerHTML = [1, 2].map(() => `
      <div class="list-row skeleton" style="height: 58px; margin-bottom: 0.5rem; opacity: 0.6;"></div>
    `).join('');
  },

  render() {
    if (!this.container) return;
    if (this.keys.length === 0) {
      this.container.innerHTML = `
        <div style="padding: 3.5rem 0; text-align: center; color: var(--text-muted); font-family: var(--font-mono); border-bottom: 1px solid var(--border-line);">
          <div style="font-size: 14px; margin-bottom: 0.5rem;">NO SSH KEYS CONFIGURED IN VAULT (servers/keys/)</div>
          <div style="font-size: 12px; color: var(--text-dim);">Click [+ GENERATE KEYPAIR] to create an Ed25519 cryptographic keypair.</div>
        </div>
      `;
      return;
    }

    this.container.innerHTML = this.keys.map((k, idx) => `
      <div class="list-row">
        <span class="list-index">${String(idx + 1).padStart(2, '0')}</span>
        <div class="row-title">
          <svg style="width: 15px; height: 15px; color: var(--accent-base); flex-shrink: 0;"><use href="#icon-key"></use></svg>
          <span class="mono"><strong>${k.name}</strong></span>
        </div>
        <div class="row-desc mono" style="font-size:12px; color:var(--text-secondary);">
          ${k.public_key ? k.public_key.substring(0, 42) + '...' : k.relative_path}
        </div>
        <div class="row-meta">
          <span class="tag-badge accent">ED25519</span>
        </div>
        <div class="row-meta mono" style="font-size:11px;">
          <span class="status-pill ${k.has_public ? 'success' : 'warning'}">
            ${k.has_public ? 'PUBKEY READY' : 'PRIVATE ONLY'}
          </span>
        </div>
        <div style="display:flex; gap:0.5rem; justify-content: flex-end;">
          ${k.public_key ? `
            <button id="btn-copy-${k.name}" class="btn-swiss sm" onclick="SSHPilotKeys.copyPublicKey('${k.name}')" title="Copy OpenSSH authorized_keys format">
              <svg class="btn-icon" style="width: 12px; height: 12px;"><use href="#icon-check"></use></svg>
              <span>COPY PUBKEY</span>
            </button>
          ` : ''}
        </div>
      </div>
    `).join('');
  },

  copyPublicKey(name) {
    const key = this.keys.find(k => k.name === name);
    if (!key || !key.public_key) return;

    const btn = document.getElementById(`btn-copy-${name}`);

    navigator.clipboard.writeText(key.public_key).then(() => {
      if (btn) {
        const span = btn.querySelector('span');
        if (span) span.textContent = 'COPIED!';
        btn.classList.add('primary');
        setTimeout(() => {
          if (span) span.textContent = 'COPY PUBKEY';
          btn.classList.remove('primary');
        }, 1600);
      }
      window.SSHPilotApp.showToast(`Copied public key for ${name} to clipboard`, 'success');
    }).catch(err => {
      window.SSHPilotApp.showToast('Clipboard access denied', 'danger');
    });
  },

  async handleGenerateKey() {
    const serverName = prompt('Enter identifier for new keypair (e.g. prod-node or dev-cluster):');
    if (!serverName) return;

    window.SSHPilotApp.showToast(`Generating secure Ed25519 keypair for ${serverName}...`, 'info');

    try {
      const res = await fetch('/api/keys', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ server_name: serverName })
      });
      const data = await res.json();
      if (data.success) {
        window.SSHPilotApp.showToast(`Generated keypair in servers/${data.key_path}`, 'success');
        this.loadKeys();
      } else {
        window.SSHPilotApp.showToast(data.error || 'Failed to generate key', 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  }
};

window.SSHPilotKeys = SSHPilotKeys;
