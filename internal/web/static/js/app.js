/**
 * SSHPilot Main App Controller, Theme Engine & Observability Orchestrator
 * Formula: State -> Feedback -> Motion -> Color -> Meaning
 */

window.SSHPilotState = {
  activeServer: null,
  activeTab: 'servers',
  latencyMs: null,
  connectionState: 'idle' // idle | connecting | online | offline
};

/* --------------------------------------------------------------------------
   THEME & ACCENT SYSTEM ENGINE
   -------------------------------------------------------------------------- */
const SSHPilotTheme = {
  presets: [
    { id: 'lime', name: 'Neon Lime', hex: '#d9f927', contrast: '#0b0c0e' },
    { id: 'cyan', name: 'Electric Cyan', hex: '#00e5ff', contrast: '#0b0c0e' },
    { id: 'blue', name: 'Cobalt Blue', hex: '#3b82f6', contrast: '#ffffff' },
    { id: 'emerald', name: 'Emerald', hex: '#10b981', contrast: '#ffffff' },
    { id: 'amber', name: 'Solar Amber', hex: '#f59e0b', contrast: '#0b0c0e' },
    { id: 'rose', name: 'Crimson Rose', hex: '#f43f5e', contrast: '#ffffff' },
    { id: 'violet', name: 'Amethyst', hex: '#a855f7', contrast: '#ffffff' },
    { id: 'silver', name: 'Silver Slate', hex: '#e2e8f0', contrast: '#0b0c0e' }
  ],

  currentTheme: 'system',
  currentAccent: '#d9f927',
  currentDensity: 'comfortable',
  currentMotion: 'system',

  init() {
    this.currentTheme = localStorage.getItem('sshpilot_theme') || 'system';
    this.currentAccent = localStorage.getItem('sshpilot_accent') || '#d9f927';
    this.currentDensity = localStorage.getItem('sshpilot_density') || 'comfortable';
    this.currentMotion = localStorage.getItem('sshpilot_motion') || 'system';

    this.applyTheme(this.currentTheme, false);
    this.applyAccent(this.currentAccent, false);
    this.applyDensity(this.currentDensity, false);
    this.applyMotion(this.currentMotion, false);

    this.renderSwatches();
    this.bindEvents();

    // Listen for OS color scheme shifts
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (this.currentTheme === 'system') {
        this.applyTheme('system', false);
      }
    });
  },

  renderSwatches() {
    const container = document.getElementById('accent-swatches-container');
    if (!container) return;

    container.innerHTML = this.presets.map(p => {
      const isActive = this.currentAccent.toLowerCase() === p.hex.toLowerCase();
      return `
        <div class="accent-swatch ${isActive ? 'active' : ''}" data-hex="${p.hex}">
          <span class="swatch-color-dot" style="background: ${p.hex};"></span>
          <span>${p.name}</span>
        </div>
      `;
    }).join('');

    container.querySelectorAll('.accent-swatch').forEach(el => {
      el.addEventListener('click', () => {
        const hex = el.dataset.hex;
        this.applyAccent(hex, true);
      });
    });

    const customInput = document.getElementById('custom-accent-input');
    const customHex = document.getElementById('custom-accent-hex');
    if (customInput) customInput.value = this.currentAccent;
    if (customHex) customHex.value = this.currentAccent.toUpperCase();
  },

  bindEvents() {
    // Theme options
    document.querySelectorAll('.theme-card').forEach(card => {
      card.addEventListener('click', () => {
        const val = card.dataset.themeVal;
        this.applyTheme(val, true);
      });
    });

    // Custom color picker
    const customInput = document.getElementById('custom-accent-input');
    const customHex = document.getElementById('custom-accent-hex');

    if (customInput) {
      customInput.addEventListener('input', (e) => {
        const hex = e.target.value;
        if (customHex) customHex.value = hex.toUpperCase();
        this.applyAccent(hex, true);
      });
    }

    if (customHex) {
      customHex.addEventListener('change', (e) => {
        let val = e.target.value.trim();
        if (!val.startsWith('#')) val = '#' + val;
        if (/^#[0-9A-Fa-f]{6}$/.test(val)) {
          if (customInput) customInput.value = val;
          this.applyAccent(val, true);
        }
      });
    }

    // Density toggle
    const densityGroup = document.getElementById('density-toggle-group');
    if (densityGroup) {
      densityGroup.querySelectorAll('.toggle-opt').forEach(btn => {
        btn.addEventListener('click', () => {
          this.applyDensity(btn.dataset.densityVal, true);
        });
      });
    }

    // Motion toggle
    const motionGroup = document.getElementById('motion-toggle-group');
    if (motionGroup) {
      motionGroup.querySelectorAll('.toggle-opt').forEach(btn => {
        btn.addEventListener('click', () => {
          this.applyMotion(btn.dataset.motionVal, true);
        });
      });
    }
  },

  applyTheme(themeVal, save = true) {
    this.currentTheme = themeVal;
    if (save) localStorage.setItem('sshpilot_theme', themeVal);

    let effectiveTheme = themeVal;
    if (themeVal === 'system') {
      effectiveTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }

    document.documentElement.setAttribute('data-theme', effectiveTheme);

    // Update UI Cards
    document.querySelectorAll('.theme-card').forEach(c => {
      const active = c.dataset.themeVal === themeVal;
      c.classList.toggle('active', active);
      const icon = c.querySelector('.theme-check-icon');
      if (icon) icon.textContent = active ? '✓' : '';
    });

    this.updateStatusTag();
  },

  applyAccent(hex, save = true) {
    this.currentAccent = hex;
    if (save) localStorage.setItem('sshpilot_accent', hex);

    const root = document.documentElement;
    const rgb = this.hexToRgb(hex);

    if (rgb) {
      // Calculate luminance
      const lum = (0.299 * rgb.r + 0.587 * rgb.g + 0.114 * rgb.b) / 255;
      const contrast = lum > 0.55 ? '#0b0c0e' : '#ffffff';

      root.style.setProperty('--accent-base', hex);
      root.style.setProperty('--accent-contrast', contrast);
      root.style.setProperty('--accent-subtle', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.14)`);
      root.style.setProperty('--accent-muted', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.22)`);
      root.style.setProperty('--accent-glow', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.35)`);
      
      // Lighten for hover
      const hoverR = Math.min(255, Math.round(rgb.r * 1.1 + 15));
      const hoverG = Math.min(255, Math.round(rgb.g * 1.1 + 15));
      const hoverB = Math.min(255, Math.round(rgb.b * 1.1 + 15));
      root.style.setProperty('--accent-hover', `rgb(${hoverR}, ${hoverG}, ${hoverB})`);
    }

    // Update swatch selections
    document.querySelectorAll('.accent-swatch').forEach(sw => {
      sw.classList.toggle('active', sw.dataset.hex.toLowerCase() === hex.toLowerCase());
    });

    this.updateStatusTag();
  },

  applyDensity(densityVal, save = true) {
    this.currentDensity = densityVal;
    if (save) localStorage.setItem('sshpilot_density', densityVal);
    document.documentElement.setAttribute('data-density', densityVal);

    const densityGroup = document.getElementById('density-toggle-group');
    if (densityGroup) {
      densityGroup.querySelectorAll('.toggle-opt').forEach(b => {
        b.classList.toggle('active', b.dataset.densityVal === densityVal);
      });
    }
  },

  applyMotion(motionVal, save = true) {
    this.currentMotion = motionVal;
    if (save) localStorage.setItem('sshpilot_motion', motionVal);
    document.documentElement.setAttribute('data-motion', motionVal);

    const motionGroup = document.getElementById('motion-toggle-group');
    if (motionGroup) {
      motionGroup.querySelectorAll('.toggle-opt').forEach(b => {
        b.classList.toggle('active', b.dataset.motionVal === motionVal);
      });
    }
  },

  updateStatusTag() {
    const tag = document.getElementById('footer-theme-tag');
    if (tag) {
      const match = this.presets.find(p => p.hex.toLowerCase() === this.currentAccent.toLowerCase());
      const accentName = match ? match.name.toUpperCase() : 'CUSTOM';
      tag.textContent = `${this.currentTheme.toUpperCase()} // ${accentName}`;
    }
  },

  hexToRgb(hex) {
    const cleanHex = hex.replace(/^#/, '');
    if (cleanHex.length !== 6) return null;
    const num = parseInt(cleanHex, 16);
    return {
      r: (num >> 16) & 255,
      g: (num >> 8) & 255,
      b: num & 255
    };
  }
};

/* --------------------------------------------------------------------------
   MAIN CONTROLLER
   -------------------------------------------------------------------------- */
const SSHPilotApp = {
  init() {
    SSHPilotTheme.init();
    this.initClock();
    this.initTabs();
    this.initMobileNav();
    this.initModals();
    this.initTerminal();
    this.initGitHubSync();
    this.initOverflowWatcher();

    // Initialize Subsystems
    if (window.SSHPilotServers) window.SSHPilotServers.init();
    if (window.SSHPilotSlashRunner) window.SSHPilotSlashRunner.init();
    if (window.SSHPilotFiles) window.SSHPilotFiles.init();
    if (window.SSHPilotKeys) window.SSHPilotKeys.init();
  },

  initClock() {
    const clock = document.getElementById('live-clock');
    const update = () => {
      const now = new Date();
      if (clock) {
        const pad = (n) => String(n).padStart(2, '0');
        clock.textContent = `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())} LOCAL`;
      }
    };
    update();
    setInterval(update, 1000);
  },

  initTabs() {
    // Desktop Nav tabs
    document.querySelectorAll('.nav-tab').forEach(tab => {
      tab.addEventListener('click', () => {
        const view = tab.dataset.view;
        this.switchTab(view);
      });
    });

    // Mobile Bottom bar buttons
    document.querySelectorAll('.mobile-bar-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const view = btn.dataset.view;
        this.switchTab(view);
      });
    });
  },

  initMobileNav() {
    const menuToggle = document.getElementById('btn-mobile-menu');
    const drawer = document.getElementById('mobile-nav-drawer');
    const backdrop = document.getElementById('mobile-nav-backdrop');
    const closeBtn = document.getElementById('btn-close-mobile-nav');
    const mobileAppearanceBtn = document.getElementById('btn-open-appearance-mobile');

    const openDrawer = () => {
      if (drawer) drawer.classList.add('active');
      if (backdrop) backdrop.classList.add('active');
    };

    const closeDrawer = () => {
      if (drawer) drawer.classList.remove('active');
      if (backdrop) backdrop.classList.remove('active');
    };

    if (menuToggle) menuToggle.addEventListener('click', openDrawer);
    if (closeBtn) closeBtn.addEventListener('click', closeDrawer);
    if (backdrop) backdrop.addEventListener('click', closeDrawer);

    // Mobile drawer navigation items
    document.querySelectorAll('.mobile-nav-item').forEach(item => {
      item.addEventListener('click', () => {
        const view = item.dataset.view;
        this.switchTab(view);
        closeDrawer();
      });
    });

    if (mobileAppearanceBtn) {
      mobileAppearanceBtn.addEventListener('click', () => {
        closeDrawer();
        const modal = document.getElementById('modal-appearance');
        if (modal) modal.classList.add('active');
      });
    }
  },

  switchTab(viewName) {
    window.SSHPilotState.activeTab = viewName;

    // Sync Desktop Tabs
    document.querySelectorAll('.nav-tab').forEach(t => {
      t.classList.toggle('active', t.dataset.view === viewName);
    });

    // Sync Mobile Bottom Bar
    document.querySelectorAll('.mobile-bar-btn').forEach(b => {
      b.classList.toggle('active', b.dataset.view === viewName);
    });

    // Sync Mobile Drawer Items
    document.querySelectorAll('.mobile-nav-item').forEach(m => {
      m.classList.toggle('active', m.dataset.view === viewName);
    });

    // Toggle Panels
    document.querySelectorAll('.view-panel').forEach(p => {
      p.classList.toggle('active', p.id === `view-${viewName}`);
    });

    if (viewName === 'files') {
      if (window.SSHPilotFiles) window.SSHPilotFiles.loadDir();
    } else if (viewName === 'scripts') {
      if (window.SSHPilotSlashRunner) window.SSHPilotSlashRunner.loadScripts();
    } else if (viewName === 'keys') {
      if (window.SSHPilotKeys) window.SSHPilotKeys.loadKeys();
    } else if (viewName === 'servers') {
      if (window.SSHPilotServers) window.SSHPilotServers.loadServers();
    } else if (viewName === 'monitoring') {
      if (window.MonitoringController) window.MonitoringController.onActivated(window.SSHPilotState.activeServer);
    } else if (viewName === 'diagnostics') {
      if (window.DiagnosticsController) window.DiagnosticsController.onActivated(window.SSHPilotState.activeServer);
    } else if (viewName === 'loadtest') {
      if (window.LoadTestController) window.LoadTestController.onActivated();
    } else if (viewName === 'console') {
      if (window.SSHPilotTerminalInstance) {
        window.SSHPilotTerminalInstance.handleResize();
      }
    }

    if (viewName !== 'monitoring' && window.MonitoringController) {
      window.MonitoringController.onDeactivated();
    }
    if (viewName !== 'loadtest' && window.LoadTestController) {
      window.LoadTestController.onDeactivated();
    }

    this.checkHorizontalOverflow();
  },

  initModals() {
    // Open Appearance modal button
    const btnApp = document.getElementById('btn-open-appearance');
    if (btnApp) {
      btnApp.addEventListener('click', () => {
        const modal = document.getElementById('modal-appearance');
        if (modal) modal.classList.add('active');
      });
    }

    // Universal data-close buttons
    document.querySelectorAll('[data-close]').forEach(btn => {
      btn.addEventListener('click', () => {
        const targetId = btn.dataset.close;
        const modal = document.getElementById(targetId);
        if (modal) modal.classList.remove('active');
      });
    });

    // Close on escape key
    window.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        document.querySelectorAll('.modal-overlay.active').forEach(m => {
          m.classList.remove('active');
        });
        const drawer = document.getElementById('mobile-nav-drawer');
        const backdrop = document.getElementById('mobile-nav-backdrop');
        if (drawer) drawer.classList.remove('active');
        if (backdrop) backdrop.classList.remove('active');
      }
    });

    // Close modals on clicking overlay backdrop (outside modal dialog)
    document.querySelectorAll('.modal-overlay').forEach(overlay => {
      overlay.addEventListener('click', (e) => {
        if (e.target === overlay) {
          overlay.classList.remove('active');
        }
      });
    });
  },

  initTerminal() {
    if (typeof SSHPilotTerminal === 'function') {
      window.SSHPilotTerminalInstance = new SSHPilotTerminal('terminal-wrapper');
    }

    const clearBtn = document.getElementById('btn-term-clear');
    if (clearBtn) {
      clearBtn.addEventListener('click', () => {
        if (window.SSHPilotTerminalInstance) window.SSHPilotTerminalInstance.clear();
      });
    }

    const reconnectBtn = document.getElementById('btn-term-reconnect');
    if (reconnectBtn) {
      reconnectBtn.addEventListener('click', () => {
        if (window.SSHPilotState.activeServer) {
          if (window.SSHPilotTerminalInstance) window.SSHPilotTerminalInstance.connect(window.SSHPilotState.activeServer);
        } else {
          SSHPilotApp.showToast('Please select a target server to connect', 'warning');
        }
      });
    }

    // Mobile Terminal Touch Accessory Bar Handlers
    document.querySelectorAll('.term-key-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        if (!window.SSHPilotTerminalInstance) return;
        const key = btn.dataset.key;
        const ctrl = btn.dataset.ctrl;

        if (key === 'Escape') {
          window.SSHPilotTerminalInstance.sendRaw('\x1b');
        } else if (key === 'Backspace') {
          window.SSHPilotTerminalInstance.sendRaw('\x7f');
        } else if (key === 'Tab') {
          window.SSHPilotTerminalInstance.sendRaw('\t');
        } else if (key === '/') {
          if (window.SSHPilotSlashRunner) window.SSHPilotSlashRunner.openPalette();
        } else if (key === 'ArrowUp') {
          window.SSHPilotTerminalInstance.sendRaw('\x1b[A');
        } else if (key === 'ArrowDown') {
          window.SSHPilotTerminalInstance.sendRaw('\x1b[B');
        } else if (key === 'ArrowLeft') {
          window.SSHPilotTerminalInstance.sendRaw('\x1b[D');
        } else if (key === 'ArrowRight') {
          window.SSHPilotTerminalInstance.sendRaw('\x1b[C');
        } else if (ctrl === 'C') {
          window.SSHPilotTerminalInstance.sendRaw('\x03');
        } else if (ctrl === 'Z') {
          window.SSHPilotTerminalInstance.sendRaw('\x1a');
        }
      });
    });
  },

  initOverflowWatcher() {
    window.addEventListener('resize', () => {
      this.checkHorizontalOverflow();
    });
  },

  checkHorizontalOverflow() {
    const docW = document.documentElement.clientWidth;
    const scrollW = document.documentElement.scrollWidth;
    if (scrollW > docW + 1) {
      console.warn(`[SSHPilot Responsive Audit] Horizontal overflow detected: scrollWidth (${scrollW}px) > clientWidth (${docW}px)`);
    }
  },

  initGitHubSync() {
    const btn = document.getElementById('btn-github-sync');
    const indicator = document.getElementById('github-sync-indicator');
    const footerSyncVal = document.getElementById('footer-sync-val');

    const triggerSync = async () => {
      if (btn) btn.disabled = true;
      if (footerSyncVal) footerSyncVal.textContent = 'CHECKING...';
      
      const refreshIcon = btn ? btn.querySelector('.btn-icon') : null;
      if (refreshIcon) refreshIcon.classList.add('icon-spin');

      try {
        const res = await fetch('/api/scripts/sync-github', { method: 'POST' });
        const data = await res.json();
        
        if (indicator) {
          indicator.innerHTML = `
            <div style="display:flex; align-items:center; gap:1rem; padding:0.75rem 1.25rem; border:1px solid var(--border-line); background:var(--bg-surface); font-family:var(--font-mono); font-size:12px; border-radius:4px;">
              <span class="status-dot online"></span>
              <span>GITHUB: <strong>${data.repo || 'github.com/ssh-pilot'}</strong> (${data.branch || 'main'})</span>
              <span style="color:var(--text-muted);">// ${data.message || 'Up to date'}</span>
              <span style="margin-left:auto; color:var(--text-muted); font-size:11px;">CHECKED: ${data.last_checked || new Date().toLocaleTimeString()}</span>
            </div>
          `;
        }
        if (footerSyncVal) footerSyncVal.textContent = 'OK';
        SSHPilotApp.showToast(data.message || 'GitHub repository sync verified', 'success');
      } catch (err) {
        if (footerSyncVal) footerSyncVal.textContent = 'ERROR';
        SSHPilotApp.showToast('GitHub sync check failed: ' + err.message, 'warning');
      } finally {
        if (btn) btn.disabled = false;
        if (refreshIcon) refreshIcon.classList.remove('icon-spin');
      }
    };

    if (btn) btn.addEventListener('click', triggerSync);
    // Auto-check once on load
    triggerSync();
  },

  updateTelemetry(serverName, status, latency) {
    window.SSHPilotState.activeServer = serverName;
    window.SSHPilotState.connectionState = status;
    window.SSHPilotState.latencyMs = latency;

    // Header updates
    const nameEl = document.getElementById('active-server-name');
    const dotEl = document.getElementById('server-status-dot');
    if (nameEl) nameEl.textContent = serverName ? serverName.toUpperCase() : 'NO SERVER SELECTED';
    if (dotEl) dotEl.className = `status-dot ${status}`;

    // Footer updates
    const footerDot = document.getElementById('footer-conn-dot');
    const footerLabel = document.getElementById('footer-conn-label');
    const footerLatency = document.getElementById('footer-latency-val');

    if (footerDot) footerDot.className = `status-dot ${status}`;
    if (footerLabel) footerLabel.textContent = serverName ? `NODE: ${serverName.toUpperCase()} [${status.toUpperCase()}]` : 'HOST: DISCONNECTED';
    if (footerLatency) footerLatency.textContent = latency ? `${latency} ms` : '-- ms';
  },

  showToast(message, type = 'info', duration = 3800) {
    const container = document.getElementById('toast-container');
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = 'toast-item';

    let iconSymbol = 'icon-activity';
    let iconColor = 'var(--text-secondary)';

    if (type === 'success') {
      iconSymbol = 'icon-check';
      iconColor = 'var(--success)';
    } else if (type === 'danger') {
      iconSymbol = 'icon-alert';
      iconColor = 'var(--danger)';
    } else if (type === 'warning') {
      iconSymbol = 'icon-alert';
      iconColor = 'var(--warning)';
    } else if (type === 'info') {
      iconSymbol = 'icon-activity';
      iconColor = 'var(--info)';
    }

    toast.innerHTML = `
      <svg class="toast-icon" style="color: ${iconColor};"><use href="#${iconSymbol}"></use></svg>
      <span class="toast-text">${message}</span>
      <button class="toast-close" title="Dismiss">✕</button>
    `;

    const closeBtn = toast.querySelector('.toast-close');
    const dismiss = () => {
      toast.classList.remove('show');
      setTimeout(() => toast.remove(), 240);
    };

    if (closeBtn) closeBtn.addEventListener('click', dismiss);

    container.appendChild(toast);
    // Trigger animation next tick
    requestAnimationFrame(() => toast.classList.add('show'));

    setTimeout(dismiss, duration);
  }
};

window.SSHPilot = SSHPilotApp;
window.SSHPilotApp = SSHPilotApp;
window.SSHPilotTheme = SSHPilotTheme;

document.addEventListener('DOMContentLoaded', () => {
  SSHPilotApp.init();
});
