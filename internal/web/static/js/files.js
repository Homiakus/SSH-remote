/**
 * SSHPilot SFTP Remote Filesystem Manager
 * Icons & Microinteraction Enhanced
 */

const SSHPilotFiles = {
  currentPath: '/',
  entries: [],
  isLoading: false,

  init() {
    this.tableBody = document.getElementById('fs-table-body');
    this.pathDisplay = document.getElementById('fs-current-path');
    this.uploadBtn = document.getElementById('btn-fs-upload');
    this.uploadInput = document.getElementById('fs-upload-input');
    this.mkdirBtn = document.getElementById('btn-fs-mkdir');
    this.editorModal = document.getElementById('modal-file-editor');
    this.saveFileBtn = document.getElementById('btn-save-file');
    this.editingFilePath = null;

    if (this.uploadBtn && this.uploadInput) {
      this.uploadBtn.addEventListener('click', () => this.uploadInput.click());
      this.uploadInput.addEventListener('change', (e) => this.handleUpload(e));
    }

    if (this.mkdirBtn) {
      this.mkdirBtn.addEventListener('click', () => this.handleMkdir());
    }

    if (this.saveFileBtn) {
      this.saveFileBtn.addEventListener('click', () => this.handleSaveEditedFile());
    }
  },

  async loadDir(dirPath) {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) {
      if (this.tableBody) {
        this.tableBody.innerHTML = `
          <tr>
            <td colspan="5" style="padding:3rem; text-align:center; color:var(--text-muted); font-family:var(--font-mono);">
              SELECT A TARGET SERVER IN 01 // SERVERS TO BROWSE REMOTE FILESYSTEM
            </td>
          </tr>
        `;
      }
      return;
    }

    this.currentPath = dirPath || this.currentPath || '/';
    if (this.pathDisplay) this.pathDisplay.textContent = this.currentPath;

    this.renderSkeleton();

    try {
      const res = await fetch(`/api/fs/list?server=${encodeURIComponent(activeServer)}&path=${encodeURIComponent(this.currentPath)}`);
      const data = await res.json();
      if (data.entries) {
        this.entries = data.entries;
        this.render();
      } else {
        window.SSHPilotApp.showToast(data.error || 'Failed to list directory', 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  renderSkeleton() {
    if (!this.tableBody) return;
    this.tableBody.innerHTML = [1, 2, 3, 4].map(() => `
      <tr>
        <td colspan="5" style="padding: 0.75rem 1rem;">
          <div class="skeleton" style="height: 24px; width: 100%;"></div>
        </td>
      </tr>
    `).join('');
  },

  render() {
    if (!this.tableBody) return;

    let rowsHtml = '';

    // Parent directory navigation
    if (this.currentPath !== '/' && this.currentPath !== '') {
      const parent = this.currentPath.split('/').slice(0, -1).join('/') || '/';
      rowsHtml += `
        <tr>
          <td>
            <span class="fs-item-name" onclick="SSHPilotFiles.loadDir('${parent}')">
              <svg class="fs-icon dir"><use href="#icon-folder"></use></svg>
              <span>.. [Parent Directory]</span>
            </span>
          </td>
          <td class="mono">drwxr-xr-x</td>
          <td class="mono">--</td>
          <td class="mono">--</td>
          <td></td>
        </tr>
      `;
    }

    if (this.entries.length === 0) {
      rowsHtml += `
        <tr>
          <td colspan="5" style="padding:2.5rem; text-align:center; color:var(--text-muted); font-family:var(--font-mono);">
            DIRECTORY IS EMPTY
          </td>
        </tr>
      `;
    } else {
      this.entries.forEach(e => {
        const isDir = e.IsDir || e.is_dir;
        const iconClass = isDir ? 'fs-icon dir' : 'fs-icon';
        const iconUse = isDir ? '#icon-folder' : '#icon-terminal';
        
        const clickAction = isDir 
          ? `SSHPilotFiles.loadDir('${e.Path || (this.currentPath + '/' + e.Name)}')` 
          : `SSHPilotFiles.openEditor('${e.Path || (this.currentPath + '/' + e.Name)}')`;
        
        rowsHtml += `
          <tr>
            <td>
              <span class="fs-item-name" onclick="${clickAction}">
                <svg class="${iconClass}"><use href="${iconUse}"></use></svg>
                <span>${e.Name || e.name}</span>
              </span>
            </td>
            <td class="mono">${this.formatMode(e.Mode || e.mode)}</td>
            <td class="mono">${this.formatSize(e.Size || e.size || 0)}</td>
            <td class="mono">${e.ModTime ? new Date(e.ModTime).toLocaleDateString() : '--'}</td>
            <td style="text-align: right;">
              <button class="btn-swiss sm danger" onclick="SSHPilotFiles.deleteItem('${e.Path || (this.currentPath + '/' + e.Name)}')">DEL</button>
            </td>
          </tr>
        `;
      });
    }

    this.tableBody.innerHTML = rowsHtml;
  },

  async openEditor(filePath) {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) return;

    try {
      const res = await fetch(`/api/fs/read?server=${encodeURIComponent(activeServer)}&path=${encodeURIComponent(filePath)}`);
      const data = await res.json();
      if (data.file) {
        this.editingFilePath = filePath;
        document.getElementById('editor-file-title').textContent = `EDITING: ${filePath}`;
        document.getElementById('editor-file-content').value = data.file.Content || data.file.content || '';
        this.editorModal.classList.add('active');
      } else {
        window.SSHPilotApp.showToast(data.error || 'Failed to read file', 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  async handleSaveEditedFile() {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer || !this.editingFilePath) return;

    const content = document.getElementById('editor-file-content').value;

    try {
      const res = await fetch('/api/fs/write', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          server: activeServer,
          path: this.editingFilePath,
          content: content
        })
      });
      const data = await res.json();
      if (data.success) {
        this.editorModal.classList.remove('active');
        window.SSHPilotApp.showToast('File written successfully to remote', 'success');
        this.loadDir(this.currentPath);
      } else {
        window.SSHPilotApp.showToast(data.error, 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  async handleMkdir() {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) return;
    const name = prompt('Enter new directory name:');
    if (!name) return;

    const target = this.currentPath.replace(/\/$/, '') + '/' + name;
    try {
      const res = await fetch('/api/fs/mkdir', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ server: activeServer, path: target })
      });
      const data = await res.json();
      if (data.success) {
        window.SSHPilotApp.showToast(`Directory ${name} created`, 'success');
        this.loadDir(this.currentPath);
      } else {
        window.SSHPilotApp.showToast(data.error, 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  async handleUpload(e) {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) return;

    const file = e.target.files[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('server', activeServer);
    formData.append('dir', this.currentPath);
    formData.append('file', file);

    window.SSHPilotApp.showToast(`Uploading ${file.name}...`, 'info');

    try {
      const res = await fetch('/api/fs/upload', {
        method: 'POST',
        body: formData
      });
      const data = await res.json();
      if (data.success) {
        window.SSHPilotApp.showToast(`Uploaded ${file.name}`, 'success');
        this.loadDir(this.currentPath);
      } else {
        window.SSHPilotApp.showToast(data.error, 'danger');
      }
    } catch (err) {
      window.SSHPilotApp.showToast(err.message, 'danger');
    }
    e.target.value = '';
  },

  async deleteItem(itemPath) {
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) return;
    if (!confirm(`Delete ${itemPath}?`)) return;

    try {
      const res = await fetch('/api/fs/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ server: activeServer, path: itemPath })
      });
      const data = await res.json();
      if (data.success) {
        window.SSHPilotApp.showToast('Item deleted from remote host', 'success');
        this.loadDir(this.currentPath);
      } else {
        window.SSHPilotApp.showToast(data.error, 'danger');
      }
    } catch (e) {
      window.SSHPilotApp.showToast(e.message, 'danger');
    }
  },

  formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  },

  formatMode(mode) {
    if (typeof mode === 'number') {
      return (mode & 0o777).toString(8).padStart(3, '0');
    }
    return mode || '0644';
  }
};

window.SSHPilotFiles = SSHPilotFiles;
