/**
 * SSHPilot Slash Command & Multi-Stage Deployment Runner
 * Handles `/` command execution pipeline:
 * [01/04] GitHub version check -> [02/04] Payload staging -> [03/04] SFTP transfer -> [04/04] Chmod & Execute
 */

const SSHPilotSlashRunner = {
  packages: [],
  allScripts: [],
  selectedIndex: 0,
  filteredScripts: [],

  init() {
    this.modalPalette = document.getElementById('modal-slash-palette');
    this.modalRunner = document.getElementById('modal-pipeline-runner');
    this.searchInput = document.getElementById('palette-search-input');
    this.resultsContainer = document.getElementById('palette-results-list');
    this.triggerBtn = document.getElementById('btn-trigger-slash');

    if (this.triggerBtn) {
      this.triggerBtn.addEventListener('click', () => this.openPalette());
    }

    if (this.searchInput) {
      this.searchInput.addEventListener('input', () => this.handleSearch());
      this.searchInput.addEventListener('keydown', (e) => this.handleKeyNav(e));
    }

    // Global shortcut listener for `/`
    window.addEventListener('keydown', (e) => {
      if (e.key === '/' && !this.isInputFocused() && !this.modalPalette.classList.contains('active')) {
        e.preventDefault();
        this.openPalette();
      }
    });

    this.loadScripts();
  },

  isInputFocused() {
    const tag = document.activeElement ? document.activeElement.tagName.toLowerCase() : '';
    return tag === 'input' || tag === 'textarea' || tag === 'select';
  },

  async loadScripts() {
    try {
      const res = await fetch('/api/scripts');
      const data = await res.json();
      this.packages = data.packages || [];
      this.allScripts = [];
      this.packages.forEach(pkg => {
        (pkg.scripts || []).forEach(s => {
          this.allScripts.push({
            ...s,
            pkgName: pkg.Name || pkg.name
          });
        });
      });
      this.renderScriptsView();
    } catch (e) {
      console.error('Failed to load scripts', e);
    }
  },

  openPalette() {
    this.modalPalette.classList.add('active');
    this.searchInput.value = '';
    this.filterScripts('');
    setTimeout(() => this.searchInput.focus(), 50);
  },

  closePalette() {
    this.modalPalette.classList.remove('active');
  },

  handleSearch() {
    const q = this.searchInput.value.trim();
    this.filterScripts(q);
  },

  filterScripts(query) {
    const q = query.toLowerCase().replace(/^\//, '');
    if (!q) {
      this.filteredScripts = [...this.allScripts];
    } else {
      this.filteredScripts = this.allScripts.filter(s => 
        (s.name && s.name.toLowerCase().includes(q)) || 
        (s.package && s.package.toLowerCase().includes(q)) ||
        (s.kind && s.kind.toLowerCase().includes(q))
      );
    }
    this.selectedIndex = 0;
    this.renderPaletteResults();
  },

  renderPaletteResults() {
    if (!this.resultsContainer) return;
    if (this.filteredScripts.length === 0) {
      this.resultsContainer.innerHTML = `
        <div style="padding: 1.5rem; text-align: center; color: var(--text-muted); font-family: var(--font-mono); font-size: 12px;">
          NO RUNNABLES MATCHED SEARCH QUERY
        </div>
      `;
      return;
    }

    this.resultsContainer.innerHTML = this.filteredScripts.map((s, idx) => `
      <div class="palette-item ${idx === this.selectedIndex ? 'selected' : ''}" data-idx="${idx}">
        <div class="palette-item-left">
          <span class="palette-item-title">${s.package} / ${s.name}</span>
          <span class="palette-item-sub mono">${s.entry_path || s.remote_path || 'autonomous script'}</span>
        </div>
        <div style="display:flex; align-items:center; gap:0.5rem;">
          <span class="tag-badge ${s.kind === 'go' ? 'accent' : ''}">${s.kind}</span>
          <span class="mono" style="font-size:11px; color:var(--text-muted);">${s.chmod || '0755'}</span>
        </div>
      </div>
    `).join('');

    this.resultsContainer.querySelectorAll('.palette-item').forEach(item => {
      item.addEventListener('click', () => {
        const idx = parseInt(item.dataset.idx, 10);
        this.selectScript(this.filteredScripts[idx]);
      });
    });
  },

  handleKeyNav(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      this.selectedIndex = (this.selectedIndex + 1) % this.filteredScripts.length;
      this.renderPaletteResults();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      this.selectedIndex = (this.selectedIndex - 1 + this.filteredScripts.length) % this.filteredScripts.length;
      this.renderPaletteResults();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (this.filteredScripts[this.selectedIndex]) {
        this.selectScript(this.filteredScripts[this.selectedIndex]);
      }
    } else if (e.key === 'Escape') {
      this.closePalette();
    }
  },

  selectScript(script) {
    this.closePalette();
    const activeServer = window.SSHPilotState ? window.SSHPilotState.activeServer : null;
    if (!activeServer) {
      window.SSHPilotApp.showToast('Please select a target server in 01 // SERVERS before running', 'warning');
      window.SSHPilotApp.switchTab('servers');
      return;
    }
    this.executePipeline(activeServer, script);
  },

  async executePipeline(serverName, script) {
    this.modalRunner.classList.add('active');
    document.getElementById('pipeline-modal-title').textContent = `RUNNABLE: ${script.package} / ${script.name}`;
    document.getElementById('pipeline-target-server').textContent = `TARGET: ${serverName.toUpperCase()}`;
    
    const outputElem = document.getElementById('pipeline-output-log');
    const progressFill = document.getElementById('pipeline-progress-fill');
    
    outputElem.textContent = `[PIPELINE INITIALIZING]: ${script.package}/${script.name} on ${serverName}...\n`;
    if (progressFill) progressFill.style.width = '15%';

    // Reset steps
    for (let i = 1; i <= 4; i++) {
      const node = document.getElementById(`step-node-${i}`);
      if (node) node.className = 'step-node';
      const desc = document.getElementById(`step-desc-${i}`);
      if (desc) desc.textContent = 'Pending';
    }

    // Step 1: Active
    const node1 = document.getElementById('step-node-1');
    if (node1) node1.className = 'step-node active';
    const desc1 = document.getElementById(`step-desc-1`);
    if (desc1) desc1.textContent = 'Checking GitHub SHA...';

    try {
      if (progressFill) progressFill.style.width = '35%';

      const res = await fetch('/api/scripts/execute', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          server: serverName,
          package: script.package,
          script: script.name
        })
      });

      const result = await res.json();

      if (progressFill) progressFill.style.width = '85%';

      if (result.steps) {
        result.steps.forEach(step => {
          const node = document.getElementById(`step-node-${step.step}`);
          if (node) {
            node.className = `step-node ${step.status}`;
            const desc = document.getElementById(`step-desc-${step.step}`);
            if (desc) desc.textContent = step.message || step.status;
          }
        });
      }

      if (result.output) {
        outputElem.textContent = result.output;
      } else if (result.error) {
        outputElem.textContent += `\n[ERROR]: ${result.error}`;
      } else {
        outputElem.textContent += `\nExecution completed.`;
      }

      if (result.success) {
        if (progressFill) progressFill.style.width = '100%';
        window.SSHPilotApp.showToast(`Runnable ${script.name} completed successfully`, 'success');
      } else {
        window.SSHPilotApp.showToast(`Execution halted: ${result.error || 'Check logs'}`, 'danger');
      }
    } catch (err) {
      const nodeErr = document.getElementById('step-node-1');
      if (nodeErr) nodeErr.className = 'step-node error';
      outputElem.textContent += `\n[NETWORK EXCEPTION]: ${err.message}`;
      window.SSHPilotApp.showToast(err.message, 'danger');
    }
  },

  renderScriptsView() {
    const container = document.getElementById('scripts-list-container');
    if (!container) return;

    if (this.allScripts.length === 0) {
      container.innerHTML = `
        <div style="padding:3rem 0; text-align:center; color:var(--text-muted); font-family:var(--font-mono);">
          NO RUNNABLES FOUND IN LIBRARY (scripts/ DIRECTORY)
        </div>
      `;
      return;
    }

    container.innerHTML = this.allScripts.map((s, idx) => `
      <div class="list-row">
        <span class="list-index">${String(idx + 1).padStart(2, '0')}</span>
        <div class="row-title">
          <span class="mono">${s.package} / <strong>${s.name}</strong></span>
        </div>
        <div class="row-desc mono" style="font-size:12px;">
          ${s.entry_path || s.remote_path || 'Autonomous script'}
        </div>
        <div class="row-meta">
          <span class="tag-badge ${s.kind === 'go' ? 'accent' : ''}">${s.kind}</span>
        </div>
        <div class="row-meta mono" style="font-size:11px; color:var(--text-muted);">
          SHA: ${s.checksum ? s.checksum.substring(0, 8) : 'cached'}
        </div>
        <div style="display:flex; gap:0.5rem; justify-content: flex-end;">
          <button class="btn-swiss sm primary" onclick="SSHPilotSlashRunner.selectScript(SSHPilotSlashRunner.allScripts[${idx}])" title="Execute Pipeline">
            / RUN
          </button>
        </div>
      </div>
    `).join('');
  }
};

window.SSHPilotSlashRunner = SSHPilotSlashRunner;
