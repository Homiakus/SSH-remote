/**
 * SSHPilot Servers Fleet Manager
 * Status & Microinteraction Enhanced
 */

const SSHPilotServers = {
  servers: [],
  isLoading: false,

  init() {
    this.container = document.getElementById('servers-list-container');
    this.addBtn = document.getElementById('btn-add-server');
    this.modalForm = document.getElementById('modal-server-form');
    this.form = document.getElementById('server-form');
    this.authSelect = document.getElementById('sf-auth-method');

    if (this.addBtn) {
      this.addBtn.addEventListener('click', () => this.openAddModal());
    }

    if (this.authSelect) {
      this.authSelect.addEventListener('change', () => {
        const isKey = this.authSelect.value === 'key';
        document.getElementById('sf-keypath-group').style.display = isKey ? 'flex' : 'none';
        document.getElementById('sf-password-group').style.display = isKey ? 'none' : 'flex';
      });
    }

    if (this.form) {
      this.form.addEventListener('submit', (e) => this.handleSaveServer(e));
    }

    this.loadServers();
  },

  async loadServers() {
    this.isLoading = true;
    this.renderSkeleton();

    try {
      const res = await fetch('/api/servers');
      const data = await res.json();
      this.servers = data.servers || [];
      this.isLoading = false;
      this.render();
    } catch (e) {
      this.isLoading = false;
      console.error('Failed to load servers', e);
      window.SSHPilotApp.showToast('Failed to load server configurations', 'danger');
      if (this.container) {
        this.container.innerHTML = `
          <div style="padding: 2.5rem 0; color: var(--danger); font-family: var(--font-mono); border-bottom: 1px solid var(--border-line);">
            ERROR LOADING NODE CONFIGURATIONS. IS SSHPILOT BACKEND RUNNING?
          </div>
        `;
      }
    }
  },

  renderSkeleton() {
    if (!this.container) return;
    this.container.innerHTML = [1, 2, 3].map(() => `
      <div class="list-row skeleton" style="height: 58px; margin-bottom: 0.5rem; opacity: 0.6;"></div>
    `).join('');
  },

  render() {
    if (!this.container) return;
    if (this.servers.length === 0) {
      this.container.innerHTML = `
        <div style="padding: 3.5rem 0; text-align: center; color: var(--text-muted); font-family: var(--font-mono); border-bottom: 1px solid var(--border-line);">
          <div style="font-size: 14px; margin-bottom: 0.5rem;">NO INFRASTRUCTURE NODES CONFIGURED</div>
          <div style="font-size: 12px; color: var(--text-dim);">Click [+ ADD SERVER] above to configure your first remote endpoint.</div>
        </div>
      `;
      return;
    }

    this.container.innerHTML = this.servers.map((s, idx) => {
      const isCurrentActive = window.SSHPilotState.activeServer === s.name;
      return `
        <div class="list-row ${isCurrentActive ? 'active-row' : ''}" id="server-row-${s.name}">
          <span class="list-index">${String(idx + 1).padStart(2, '0')}</span>
          <div class="row-title">
            <span class="status-dot ${isCurrentActive ? 'online' : ''}" id="dot-${s.name}"></span>
            <span class="mono"><strong>${s.name}</strong></span>
          </div>
          <div class="row-desc mono">
            ${s.user}@${s.host}:${s.port}
            <span style="color:var(--text-muted); font-size:11px; margin-left:0.5rem;">${s.description || ''}</span>
          </div>
          <div class="row-meta">
            <span class="tag-badge ${s.auth_method === 'key' ? 'accent' : ''}">
              ${s.auth_method === 'key' ? 'KEYPAIR' : 'PASSWORD'}
            </span>
          </div>
          <div class="row-meta mono" id="latency-${s.name}">
            <span class="status-pill" style="font-size: 10px;">-- ms</span>
          </div>
          <div style="display: flex; gap: 0.5rem; justify-content: flex-end;">
            <button class="btn-swiss sm primary" onclick="SSHPilotServers.connect('${s.name}')" title="Connect Terminal Session">CONNECT</button>
            <button class="btn-swiss sm" onclick="SSHPilotServers.testConnection('${s.name}')" title="Ping & SSH Handshake Test">TEST</button>
            <button class="btn-swiss sm danger" onclick="SSHPilotServers.deleteServer('${s.name}')" title="Delete Configuration">DEL</button>
          </div>
        </div>
      `;
    }).join('');
  },

  connect(name) {
    window.SSHPilotApp.updateTelemetry(name, 'online', null);
    window.SSHPilotApp.switchTab('console');
    if (window.SSHPilotTerminalInstance) {
      window.SSHPilotTerminalInstance.connect(name);
    }
  },

  async testConnection(name) {
    const latElem = document.getElementById(`latency-${name}`);
    const dotElem = document.getElementById(`dot-${name}`);
    if (latElem) {
      latElem.innerHTML = `<span class="status-pill info" style="font-size: 10px;">TESTING...</span>`;
    }
    if (dotElem) {
      dotElem.className = 'status-dot syncing';
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(name)}/test`, { method: 'POST' });
      const data = await res.json();
      if (data.success) {
        const ms = data.latency_ms;
        let pillClass = 'success';
        if (ms > 120) pillClass = 'warning';
        if (ms > 300) pillClass = 'danger';

        if (latElem) {
          latElem.innerHTML = `<span class="status-pill ${pillClass}" style="font-size: 10px;">${ms} ms</span>`;
        }
        if (dotElem) dotElem.className = 'status-dot online';

        if (window.SSHPilotState.activeServer === name) {
          window.SSHPilotApp.updateTelemetry(name, 'online', ms);
        }

        window.SSHPilotApp.showToast(`Node ${name} reachable in ${ms}ms`, 'success');
      } else {
        if (latElem) latElem.innerHTML = `<span class="status-pill danger" style="font-size: 10px;">FAILED</span>`;
        if (dotElem) dotElem.className = 'status-dot offline';
        window.SSHPilotApp.showToast(`Node ${name} unreachable: ${data.error}`, 'danger');
      }
    } catch (e) {
      if (latElem) latElem.innerHTML = `<span class="status-pill danger" style="font-size: 10px;">ERROR</span>`;
      if (dotElem) dotElem.className = 'status-dot offline';
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  openAddModal() {
    this.form.reset();
    document.getElementById('sf-name').readOnly = false;
    document.getElementById('server-form-title').textContent = 'NEW INFRASTRUCTURE NODE';
    document.getElementById('sf-keypath-group').style.display = 'flex';
    document.getElementById('sf-password-group').style.display = 'none';
    this.modalForm.classList.add('active');
  },

  async handleSaveServer(e) {
    e.preventDefault();
    const payload = {
      name: document.getElementById('sf-name').value.trim(),
      host: document.getElementById('sf-host').value.trim(),
      port: document.getElementById('sf-port').value.trim() || '22',
      user: document.getElementById('sf-user').value.trim() || 'root',
      auth_method: document.getElementById('sf-auth-method').value,
      key_path: document.getElementById('sf-keypath').value.trim(),
      password: document.getElementById('sf-password').value,
      description: document.getElementById('sf-desc').value.trim()
    };

    try {
      const res = await fetch('/api/servers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      if (data.success) {
        this.modalForm.classList.remove('active');
        window.SSHPilotApp.showToast(`Server ${payload.name} saved successfully`, 'success');
        this.loadServers();
      } else {
        window.SSHPilotApp.showToast(data.error || 'Failed to save server', 'danger');
      }
    } catch (err) {
      window.SSHPilotApp.showToast(err.message, 'danger');
    }
  },

  async deleteServer(name) {
    if (!confirm(`Are you sure you want to delete server configuration "${name}"?`)) return;

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(name)}`, { method: 'DELETE' });
      const data = await res.json();
      if (data.success) {
        window.SSHPilotApp.showToast(`Server ${name} removed`, 'success');
        this.loadServers();
      } else {
        window.SSHPilotApp.showToast(data.error, 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  }
};

window.SSHPilotServers = SSHPilotServers;
