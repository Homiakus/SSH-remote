// SSHPILOT // Monitoring Controller
(function() {
  'use strict';

  let currentServer = null;
  let refreshTimer = null;
  let refreshIntervalMs = 5000;
  let isFetching = false;

  function init() {
    setupEventListeners();
  }

  function setupEventListeners() {
    const serverSelect = document.getElementById('mon-server-select');
    if (serverSelect) {
      serverSelect.addEventListener('change', (e) => {
        currentServer = e.target.value;
        fetchMetrics(true);
      });
    }

    const intervalSelect = document.getElementById('mon-interval-select');
    if (intervalSelect) {
      intervalSelect.addEventListener('change', (e) => {
        const val = parseInt(e.target.value, 10);
        setRefreshInterval(val);
      });
    }

    const btnRefresh = document.getElementById('btn-mon-refresh');
    if (btnRefresh) {
      btnRefresh.addEventListener('click', () => {
        fetchMetrics(true);
      });
    }

    const procSearch = document.getElementById('mon-proc-search');
    if (procSearch) {
      procSearch.addEventListener('input', (e) => {
        filterProcesses(e.target.value);
      });
    }
  }

  function onActivated(serverName) {
    if (serverName && (!currentServer || currentServer !== serverName)) {
      currentServer = serverName;
    }
    populateServerDropdown();
    fetchMetrics(true);
    startAutoRefresh();
  }

  function onDeactivated() {
    stopAutoRefresh();
  }

  async function populateServerDropdown() {
    const select = document.getElementById('mon-server-select');
    if (!select) return;

    try {
      const res = await fetch('/api/servers');
      if (!res.ok) return;
      const data = await res.json();
      const servers = data.servers || [];

      select.innerHTML = '';
      if (servers.length === 0) {
        select.innerHTML = '<option value="">-- No Servers Configured --</option>';
        return;
      }

      servers.forEach(s => {
        const opt = document.createElement('option');
        opt.value = s.name;
        opt.textContent = `${s.name} (${s.host})`;
        if (s.name === currentServer) {
          opt.selected = true;
        }
        select.appendChild(opt);
      });

      if (!currentServer && servers.length > 0) {
        currentServer = servers[0].name;
        select.value = currentServer;
      }
    } catch (e) {
      console.error('Failed to populate monitoring server dropdown:', e);
    }
  }

  async function fetchMetrics(showLoading = false) {
    if (!currentServer || isFetching) return;
    isFetching = true;

    const statusEl = document.getElementById('mon-status-indicator');
    if (showLoading && statusEl) {
      statusEl.textContent = 'POLLING METRICS...';
      statusEl.className = 'mon-status-pill loading';
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/metrics`);
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${res.status}`);
      }
      const data = await res.json();
      if (data.success && data.metrics) {
        renderMetrics(data.metrics);
        if (statusEl) {
          statusEl.textContent = `LIVE // ${new Date().toLocaleTimeString()}`;
          statusEl.className = 'mon-status-pill online';
        }
      }
    } catch (err) {
      if (statusEl) {
        statusEl.textContent = `ERROR: ${err.message}`;
        statusEl.className = 'mon-status-pill offline';
      }
    } finally {
      isFetching = false;
    }
  }

  function renderMetrics(m) {
    // 1. Host Info
    const hostEl = document.getElementById('mon-host-meta');
    if (hostEl) {
      const os = m.os_info?.os_name || 'Linux';
      const kernel = m.os_info?.kernel || '';
      const arch = m.os_info?.arch || '';
      const uptime = m.os_info?.uptime_string || 'N/A';
      hostEl.innerHTML = `
        <span class="tag-badge primary">${escapeHtml(os)} ${escapeHtml(arch)}</span>
        <span class="tag-badge">${escapeHtml(kernel)}</span>
        <span class="tag-badge">UPTIME: ${escapeHtml(uptime)}</span>
      `;
    }

    // 2. CPU Usage & Load
    const cpuVal = document.getElementById('mon-cpu-val');
    const cpuBar = document.getElementById('mon-cpu-bar');
    const cpuMeta = document.getElementById('mon-cpu-meta');
    const cpuPct = m.cpu?.usage_percent || 0;
    if (cpuVal) cpuVal.textContent = `${cpuPct}%`;
    if (cpuBar) cpuBar.style.width = `${Math.min(100, Math.max(0, cpuPct))}%`;
    if (cpuMeta) {
      const cores = m.cpu?.cores || 1;
      const l1 = (m.cpu?.load_1 || 0).toFixed(2);
      const l5 = (m.cpu?.load_5 || 0).toFixed(2);
      const l15 = (m.cpu?.load_15 || 0).toFixed(2);
      cpuMeta.textContent = `${cores} Cores · Load Avg: ${l1}, ${l5}, ${l15}`;
    }

    // 3. Memory & Swap
    const memVal = document.getElementById('mon-mem-val');
    const memBar = document.getElementById('mon-mem-bar');
    const memMeta = document.getElementById('mon-mem-meta');
    const memPct = m.memory?.usage_percent || 0;
    if (memVal) memVal.textContent = `${memPct}%`;
    if (memBar) memBar.style.width = `${Math.min(100, Math.max(0, memPct))}%`;
    if (memMeta) {
      const usedGB = formatBytes(m.memory?.used_bytes || 0);
      const totalGB = formatBytes(m.memory?.total_bytes || 0);
      const availGB = formatBytes(m.memory?.available_bytes || 0);
      memMeta.textContent = `${usedGB} / ${totalGB} (Free: ${availGB})`;
    }

    // 4. Disk Storage
    const diskContainer = document.getElementById('mon-disk-list');
    if (diskContainer) {
      const disks = m.disks || [];
      if (disks.length === 0) {
        diskContainer.innerHTML = '<div class="mon-empty-sub">No mount points reported.</div>';
      } else {
        diskContainer.innerHTML = disks.map(d => {
          const usedStr = formatBytes(d.used_bytes);
          const totalStr = formatBytes(d.total_bytes);
          const pct = Math.min(100, Math.max(0, d.usage_percent));
          const barClass = pct > 85 ? 'danger' : pct > 70 ? 'warning' : 'primary';
          return `
            <div class="mon-sub-item">
              <div class="mon-sub-header">
                <span class="mono font-bold">${escapeHtml(d.mount_point)}</span>
                <span class="mono micro-label">${usedStr} / ${totalStr} (${pct}%)</span>
              </div>
              <div class="mon-progress-track">
                <div class="mon-progress-fill ${barClass}" style="width: ${pct}%;"></div>
              </div>
            </div>
          `;
        }).join('');
      }
    }

    // 5. Network I/O
    const netRx = document.getElementById('mon-net-rx');
    const netTx = document.getElementById('mon-net-tx');
    if (netRx) netRx.textContent = formatBytes(m.network?.rx_bytes || 0);
    if (netTx) netTx.textContent = formatBytes(m.network?.tx_bytes || 0);

    // 6. Processes
    window.__currentMonProcesses = m.processes || [];
    renderProcesses(window.__currentMonProcesses);

    // 7. Services
    const svcContainer = document.getElementById('mon-services-grid');
    if (svcContainer) {
      const svcs = m.services || [];
      if (svcs.length === 0) {
        svcContainer.innerHTML = '<div class="mon-empty-sub">No known service units detected.</div>';
      } else {
        svcContainer.innerHTML = svcs.map(s => {
          const isActive = s.status === 'active';
          return `
            <div class="mon-svc-card ${isActive ? 'active' : 'inactive'}">
              <div class="mon-svc-header">
                <span class="status-dot ${isActive ? 'online' : 'offline'}"></span>
                <span class="mono font-bold">${escapeHtml(s.name)}</span>
              </div>
              <div class="mon-svc-actions">
                <button class="btn-swiss xs btn-svc-action" data-service="${escapeHtml(s.name)}" data-action="restart">RESTART</button>
                <button class="btn-swiss xs ${isActive ? 'danger' : 'primary'} btn-svc-action" data-service="${escapeHtml(s.name)}" data-action="${isActive ? 'stop' : 'start'}">${isActive ? 'STOP' : 'START'}</button>
              </div>
            </div>
          `;
        }).join('');

        // Attach action handlers
        svcContainer.querySelectorAll('.btn-svc-action').forEach(btn => {
          btn.addEventListener('click', (e) => {
            const svc = e.currentTarget.dataset.service;
            const action = e.currentTarget.dataset.action;
            executeServiceAction(svc, action);
          });
        });
      }
    }
  }

  function renderProcesses(processes) {
    const tbody = document.getElementById('mon-proc-table-body');
    if (!tbody) return;

    if (processes.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="table-empty">No processes reported.</td></tr>';
      return;
    }

    tbody.innerHTML = processes.map(p => {
      return `
        <tr>
          <td class="mono font-bold">${p.pid}</td>
          <td><span class="tag-badge xs">${escapeHtml(p.user)}</span></td>
          <td class="mono font-bold ${p.cpu_percent > 50 ? 'text-danger' : ''}">${p.cpu_percent}%</td>
          <td class="mono">${p.mem_percent}%</td>
          <td class="mono text-truncate" style="max-width: 280px;" title="${escapeHtml(p.command)}">${escapeHtml(p.command)}</td>
          <td style="text-align: right;">
            <button class="btn-swiss xs danger btn-kill-proc" data-pid="${p.pid}">KILL</button>
          </td>
        </tr>
      `;
    }).join('');

    tbody.querySelectorAll('.btn-kill-proc').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const pid = parseInt(e.currentTarget.dataset.pid, 10);
        killProcess(pid);
      });
    });
  }

  function filterProcesses(query) {
    if (!window.__currentMonProcesses) return;
    const q = (query || '').toLowerCase().trim();
    if (!q) {
      renderProcesses(window.__currentMonProcesses);
      return;
    }
    const filtered = window.__currentMonProcesses.filter(p => {
      return p.pid.toString().includes(q) ||
             p.user.toLowerCase().includes(q) ||
             p.command.toLowerCase().includes(q);
    });
    renderProcesses(filtered);
  }

  async function killProcess(pid) {
    if (!currentServer || !pid) return;
    if (!confirm(`Are you sure you want to terminate process PID ${pid} on ${currentServer}?`)) {
      return;
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/process/kill`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pid: pid, force: true })
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || 'Failed to kill process');
      }
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Process PID ${pid} terminated`, 'success');
      }
      fetchMetrics(false);
    } catch (err) {
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Kill error: ${err.message}`, 'error');
      }
    }
  }

  async function executeServiceAction(service, action) {
    if (!currentServer || !service) return;

    try {
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Executing systemctl ${action} ${service}...`, 'info');
      }
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/service/action`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ service: service, action: action })
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || 'Failed to control service');
      }
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Service ${service} ${action} succeeded`, 'success');
      }
      fetchMetrics(false);
    } catch (err) {
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Service action error: ${err.message}`, 'error');
      }
    }
  }

  function setRefreshInterval(ms) {
    refreshIntervalMs = ms;
    startAutoRefresh();
  }

  function startAutoRefresh() {
    stopAutoRefresh();
    if (refreshIntervalMs > 0) {
      refreshTimer = setInterval(() => {
        fetchMetrics(false);
      }, refreshIntervalMs);
    }
  }

  function stopAutoRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
  }

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  window.MonitoringController = {
    init,
    onActivated,
    onDeactivated,
    refresh: fetchMetrics
  };

  document.addEventListener('DOMContentLoaded', init);
})();
