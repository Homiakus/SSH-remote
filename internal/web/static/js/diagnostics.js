// SSHPILOT // Diagnostics Controller
(function() {
  'use strict';

  let currentServer = null;
  let isAuditing = false;

  function init() {
    setupEventListeners();
  }

  function setupEventListeners() {
    const serverSelect = document.getElementById('diag-server-select');
    if (serverSelect) {
      serverSelect.addEventListener('change', (e) => {
        currentServer = e.target.value;
      });
    }

    const btnRunAudit = document.getElementById('btn-diag-run-audit');
    if (btnRunAudit) {
      btnRunAudit.addEventListener('click', () => {
        runFullAudit();
      });
    }

    const btnPing = document.getElementById('btn-diag-ping');
    if (btnPing) {
      btnPing.addEventListener('click', () => {
        runPingJitter();
      });
    }

    const btnFetchLogs = document.getElementById('btn-diag-fetch-logs');
    if (btnFetchLogs) {
      btnFetchLogs.addEventListener('click', () => {
        fetchRemoteLogs();
      });
    }

    const logTypeSelect = document.getElementById('diag-log-type');
    if (logTypeSelect) {
      logTypeSelect.addEventListener('change', () => {
        fetchRemoteLogs();
      });
    }

    const logSearch = document.getElementById('diag-log-search');
    if (logSearch) {
      logSearch.addEventListener('input', (e) => {
        filterLogs(e.target.value);
      });
    }

    // Diagnostic tool buttons
    document.querySelectorAll('.btn-diag-cmd').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const cmdKey = e.currentTarget.dataset.cmd;
        executeDiagnosticCommand(cmdKey);
      });
    });
  }

  function onActivated(serverName) {
    if (serverName && (!currentServer || currentServer !== serverName)) {
      currentServer = serverName;
    }
    populateServerDropdown();
  }

  async function populateServerDropdown() {
    const select = document.getElementById('diag-server-select');
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
      console.error('Failed to populate diagnostics server dropdown:', e);
    }
  }

  async function runFullAudit() {
    if (!currentServer || isAuditing) return;
    isAuditing = true;

    const timeline = document.getElementById('diag-audit-timeline');
    const bannerBox = document.getElementById('diag-banner-box');
    const keyBox = document.getElementById('diag-key-box');

    if (timeline) {
      timeline.innerHTML = '<div class="diag-loading-indicator"><div class="spinner"></div> Probing TCP, SSH Banner, HostKey, SFTP & Jitter...</div>';
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/diagnostics/run`, {
        method: 'POST'
      });
      const data = await res.json();
      const report = data.report;

      if (!report) {
        throw new Error(data.error || 'Failed to get diagnostic report');
      }

      renderAuditReport(report);
      if (bannerBox) {
        bannerBox.textContent = report.banner || 'No banner returned';
      }
      if (keyBox) {
        keyBox.textContent = report.host_key_sha256 ? `${report.host_key_alg || 'Key'} // ${report.host_key_sha256}` : 'Not extracted';
      }
    } catch (err) {
      if (timeline) {
        timeline.innerHTML = `<div class="diag-error-box">Audit Error: ${escapeHtml(err.message)}</div>`;
      }
    } finally {
      isAuditing = false;
    }
  }

  function renderAuditReport(rep) {
    const timeline = document.getElementById('diag-audit-timeline');
    if (!timeline) return;

    const stages = rep.stages || [];
    timeline.innerHTML = `
      <div class="diag-summary-header">
        <span class="status-dot ${rep.overall_status === 'pass' ? 'online' : rep.overall_status === 'warn' ? 'warning' : 'offline'}"></span>
        <span class="mono font-bold">AUDIT COMPLETE // ${rep.overall_status.toUpperCase()}</span>
        <span class="mono micro-label" style="margin-left: auto;">TOTAL: ${rep.total_duration_ms} ms</span>
      </div>
      <div class="diag-stages-list">
        ${stages.map((st, idx) => {
          const badgeClass = st.status === 'pass' ? 'success' : st.status === 'warn' ? 'warning' : 'danger';
          return `
            <div class="diag-stage-row ${st.status}">
              <div class="diag-stage-col-num mono">0${idx+1}</div>
              <div class="diag-stage-col-info">
                <div class="diag-stage-title">
                  <span class="font-bold">${escapeHtml(st.name)}</span>
                  <span class="tag-badge xs ${badgeClass}">${st.status.toUpperCase()}</span>
                  <span class="mono micro-label" style="margin-left: auto;">${st.duration_ms} ms</span>
                </div>
                <div class="diag-stage-desc mono">${escapeHtml(st.summary)}</div>
                ${st.details ? `<div class="diag-stage-details mono micro-label">${escapeHtml(st.details)}</div>` : ''}
                ${st.error ? `<div class="diag-stage-err mono micro-label">${escapeHtml(st.error)}</div>` : ''}
              </div>
            </div>
          `;
        }).join('')}
      </div>
    `;
  }

  async function runPingJitter() {
    if (!currentServer) return;
    const container = document.getElementById('diag-ping-results');
    if (container) {
      container.innerHTML = '<div class="diag-loading-indicator"><div class="spinner"></div> Measuring 10-probe latency & jitter...</div>';
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/diagnostics/ping?count=10`);
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || 'Ping test failed');
      }

      const p = data.ping;
      renderPingResults(p);
    } catch (err) {
      if (container) {
        container.innerHTML = `<div class="diag-error-box">Ping Error: ${escapeHtml(err.message)}</div>`;
      }
    }
  }

  function renderPingResults(p) {
    const container = document.getElementById('diag-ping-results');
    if (!container) return;

    const maxLat = Math.max(1, ...p.samples.map(s => s.latency_ms));

    container.innerHTML = `
      <div class="diag-ping-metrics-row">
        <div class="metric-card-mini">
          <span class="micro-label">AVG LATENCY</span>
          <span class="val mono">${p.avg_ms} ms</span>
        </div>
        <div class="metric-card-mini">
          <span class="micro-label">JITTER (±)</span>
          <span class="val mono">${p.jitter_ms} ms</span>
        </div>
        <div class="metric-card-mini">
          <span class="micro-label">MIN / MAX</span>
          <span class="val mono">${p.min_ms} / ${p.max_ms} ms</span>
        </div>
        <div class="metric-card-mini">
          <span class="micro-label">PACKET LOSS</span>
          <span class="val mono ${p.loss_percent > 0 ? 'text-danger' : 'text-success'}">${p.loss_percent}%</span>
        </div>
      </div>
      <div class="diag-jitter-chart">
        ${p.samples.map(s => {
          const heightPct = Math.min(100, Math.max(10, (s.latency_ms / maxLat) * 100));
          return `
            <div class="diag-jitter-bar-wrap" title="Probe #${s.sequence}: ${s.latency_ms} ms">
              <div class="diag-jitter-bar ${s.success ? '' : 'failed'}" style="height: ${heightPct}%;"></div>
              <span class="mono micro-label">${s.sequence}</span>
            </div>
          `;
        }).join('')}
      </div>
    `;
  }

  async function fetchRemoteLogs() {
    if (!currentServer) return;
    const logContainer = document.getElementById('diag-log-entries');
    const logTypeSelect = document.getElementById('diag-log-type');
    const logType = logTypeSelect ? logTypeSelect.value : 'journal';

    if (logContainer) {
      logContainer.innerHTML = '<div class="diag-loading-indicator"><div class="spinner"></div> Fetching remote system logs...</div>';
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/diagnostics/logs?type=${encodeURIComponent(logType)}&lines=100`);
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || 'Failed to fetch logs');
      }

      window.__currentRemoteLogs = data.entries || [];
      renderLogs(window.__currentRemoteLogs);
    } catch (err) {
      if (logContainer) {
        logContainer.innerHTML = `<div class="diag-error-box">Log Error: ${escapeHtml(err.message)}</div>`;
      }
    }
  }

  function renderLogs(entries) {
    const container = document.getElementById('diag-log-entries');
    const countEl = document.getElementById('diag-log-count');
    if (!container) return;

    if (countEl) {
      countEl.textContent = `${entries.length} LINES`;
    }

    if (entries.length === 0) {
      container.innerHTML = '<div class="diag-empty-sub">No matching log lines found.</div>';
      return;
    }

    container.innerHTML = entries.map(e => {
      const levelClass = e.level === 'error' ? 'log-err' : e.level === 'warn' ? 'log-warn' : 'log-info';
      return `
        <div class="diag-log-row ${levelClass}">
          <span class="diag-log-level mono">${e.level.toUpperCase()}</span>
          ${e.timestamp ? `<span class="diag-log-ts mono">${escapeHtml(e.timestamp)}</span>` : ''}
          <span class="diag-log-msg mono">${escapeHtml(e.message || e.raw)}</span>
        </div>
      `;
    }).join('');
  }

  function filterLogs(query) {
    if (!window.__currentRemoteLogs) return;
    const q = (query || '').toLowerCase().trim();
    if (!q) {
      renderLogs(window.__currentRemoteLogs);
      return;
    }
    const filtered = window.__currentRemoteLogs.filter(e => {
      return (e.raw || '').toLowerCase().includes(q) ||
             (e.message || '').toLowerCase().includes(q) ||
             (e.level || '').toLowerCase().includes(q);
    });
    renderLogs(filtered);
  }

  async function executeDiagnosticCommand(cmdKey) {
    if (!currentServer || !cmdKey) return;
    const outBox = document.getElementById('diag-cmd-output');
    if (outBox) {
      outBox.textContent = `Running diagnostic tool [${cmdKey}] on ${currentServer}...`;
    }

    try {
      const res = await fetch(`/api/servers/${encodeURIComponent(currentServer)}/diagnostics/exec`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command_key: cmdKey })
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || 'Diagnostic command failed');
      }

      if (outBox) {
        outBox.textContent = `$ ${data.result.command}\n\n${data.result.output || '(No output produced)'}`;
      }
    } catch (err) {
      if (outBox) {
        outBox.textContent = `Error executing tool: ${err.message}`;
      }
    }
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  window.DiagnosticsController = {
    init,
    onActivated,
    runAudit: runFullAudit
  };

  document.addEventListener('DOMContentLoaded', init);
})();
