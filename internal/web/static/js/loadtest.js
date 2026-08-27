// SSHPILOT // Load Testing & Stress Benchmark Controller
(function() {
  'use strict';

  let pollInterval = null;
  let isRunning = false;

  function init() {
    setupEventListeners();
  }

  function setupEventListeners() {
    const targetTypeSelect = document.getElementById('lt-target-type');
    if (targetTypeSelect) {
      targetTypeSelect.addEventListener('change', (e) => {
        updateTargetFields(e.target.value);
      });
    }

    const concurrencySlider = document.getElementById('lt-concurrency');
    const concurrencyVal = document.getElementById('lt-concurrency-val');
    if (concurrencySlider && concurrencyVal) {
      concurrencySlider.addEventListener('input', (e) => {
        concurrencyVal.textContent = e.target.value;
      });
    }

    const btnStart = document.getElementById('btn-lt-start');
    if (btnStart) {
      btnStart.addEventListener('click', () => {
        startLoadTest();
      });
    }

    const btnStop = document.getElementById('btn-lt-stop');
    if (btnStop) {
      btnStop.addEventListener('click', () => {
        stopLoadTest();
      });
    }

    const btnExport = document.getElementById('btn-lt-export');
    if (btnExport) {
      btnExport.addEventListener('click', () => {
        exportReportJSON();
      });
    }

    const btnRefreshHistory = document.getElementById('btn-lt-refresh-history');
    if (btnRefreshHistory) {
      btnRefreshHistory.addEventListener('click', () => {
        fetchHistory();
      });
    }
  }

  function onActivated() {
    populateServerDropdown();
    fetchStatus();
    fetchHistory();
  }

  function onDeactivated() {
    if (!isRunning) {
      stopPolling();
    }
  }

  function updateTargetFields(type) {
    const httpGroup = document.getElementById('lt-http-group');
    const sshGroup = document.getElementById('lt-ssh-group');
    const cmdGroup = document.getElementById('lt-cmd-group');

    if (type === 'http') {
      if (httpGroup) httpGroup.style.display = 'block';
      if (sshGroup) sshGroup.style.display = 'none';
      if (cmdGroup) cmdGroup.style.display = 'none';
    } else if (type === 'ssh_connect') {
      if (httpGroup) httpGroup.style.display = 'none';
      if (sshGroup) sshGroup.style.display = 'block';
      if (cmdGroup) cmdGroup.style.display = 'none';
    } else if (type === 'ssh_command') {
      if (httpGroup) httpGroup.style.display = 'none';
      if (sshGroup) sshGroup.style.display = 'block';
      if (cmdGroup) cmdGroup.style.display = 'block';
    }
  }

  async function populateServerDropdown() {
    const select = document.getElementById('lt-server-select');
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
        select.appendChild(opt);
      });
    } catch (e) {
      console.error('Failed to populate load test servers:', e);
    }
  }

  async function startLoadTest() {
    if (isRunning) return;

    const targetType = document.getElementById('lt-target-type').value;
    const concurrency = parseInt(document.getElementById('lt-concurrency').value, 10) || 10;
    const totalRequests = parseInt(document.getElementById('lt-total-reqs').value, 10) || 100;
    const durationSec = parseInt(document.getElementById('lt-duration').value, 10) || 0;
    const rpsLimit = parseInt(document.getElementById('lt-rps-limit').value, 10) || 0;
    const timeoutMs = parseInt(document.getElementById('lt-timeout').value, 10) || 5000;

    const payload = {
      target_type: targetType,
      concurrency: concurrency,
      total_requests: totalRequests,
      duration_sec: durationSec,
      rps_limit: rpsLimit,
      timeout_ms: timeoutMs
    };

    if (targetType === 'http') {
      payload.url = document.getElementById('lt-http-url').value || 'http://127.0.0.1:8080/api/servers';
      payload.method = document.getElementById('lt-http-method').value || 'GET';
      const body = document.getElementById('lt-http-body').value;
      if (body) payload.body = body;
    } else {
      payload.server_name = document.getElementById('lt-server-select').value;
      if (targetType === 'ssh_command') {
        payload.command = document.getElementById('lt-ssh-cmd').value || 'echo ok';
      }
    }

    try {
      const res = await fetch('/api/loadtest/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Failed to start load test');
      }

      isRunning = true;
      toggleRunUI(true);
      startPolling();
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast('Load test started', 'success');
      }
    } catch (err) {
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast(`Error: ${err.message}`, 'error');
      }
    }
  }

  async function stopLoadTest() {
    try {
      const res = await fetch('/api/loadtest/stop', { method: 'POST' });
      const data = await res.json();
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast('Load test abort signal sent', 'info');
      }
    } catch (e) {
      console.error('Stop load test error:', e);
    }
  }

  function startPolling() {
    stopPolling();
    pollInterval = setInterval(fetchStatus, 400);
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  async function fetchStatus() {
    try {
      const res = await fetch('/api/loadtest/status');
      if (!res.ok) return;
      const data = await res.json();

      if (!data.has_job || !data.job) {
        if (isRunning) {
          isRunning = false;
          toggleRunUI(false);
          stopPolling();
        }
        return;
      }

      const job = data.job;
      window.__latestLoadTestReport = job;
      renderCockpit(job);

      if (job.done || !job.running) {
        if (isRunning) {
          isRunning = false;
          toggleRunUI(false);
          stopPolling();
          fetchHistory();
          if (window.SSHPilot && window.SSHPilot.showToast) {
            window.SSHPilot.showToast('Load test completed!', 'success');
          }
        }
      } else {
        if (!isRunning) {
          isRunning = true;
          toggleRunUI(true);
          startPolling();
        }
      }
    } catch (e) {
      console.error('Failed to fetch load test status:', e);
    }
  }

  function renderCockpit(job) {
    const rpsVal = document.getElementById('lt-res-rps');
    const sentVal = document.getElementById('lt-res-sent');
    const successVal = document.getElementById('lt-res-success');
    const failedVal = document.getElementById('lt-res-failed');
    const elapsedVal = document.getElementById('lt-res-elapsed');
    const progressBar = document.getElementById('lt-progress-bar');
    const progressPct = document.getElementById('lt-progress-pct');

    if (rpsVal) rpsVal.textContent = job.current_rps.toFixed(1);
    if (sentVal) sentVal.textContent = job.total_sent;
    if (successVal) successVal.textContent = job.total_success;
    if (failedVal) failedVal.textContent = job.total_failed;
    if (elapsedVal) elapsedVal.textContent = `${job.duration_sec.toFixed(1)}s`;

    const pct = Math.min(100, Math.max(0, job.progress_pct));
    if (progressBar) progressBar.style.width = `${pct}%`;
    if (progressPct) progressPct.textContent = `${pct}%`;

    // Percentiles
    const lat = job.latency || {};
    const setVal = (id, v) => {
      const el = document.getElementById(id);
      if (el) el.textContent = v ? `${v} ms` : '-- ms';
    };
    setVal('lt-p50', lat.p50_ms);
    setVal('lt-p90', lat.p90_ms);
    setVal('lt-p95', lat.p95_ms);
    setVal('lt-p99', lat.p99_ms);
    setVal('lt-min', lat.min_ms);
    setVal('lt-avg', lat.avg_ms);
    setVal('lt-max', lat.max_ms);

    // Status breakdown
    const codesContainer = document.getElementById('lt-status-codes');
    if (codesContainer) {
      const codes = job.status_codes || {};
      const errs = job.error_breakdown || {};
      const codeKeys = Object.keys(codes);
      const errKeys = Object.keys(errs);

      if (codeKeys.length === 0 && errKeys.length === 0) {
        codesContainer.innerHTML = '<span class="micro-label" style="color: var(--text-muted);">Awaiting responses...</span>';
      } else {
        let html = '';
        codeKeys.forEach(k => {
          const num = parseInt(k, 10);
          const cls = num >= 200 && num < 300 ? 'success' : num >= 400 ? 'danger' : 'warning';
          html += `<span class="tag-badge xs ${cls}">${k}: ${codes[k]}</span> `;
        });
        errKeys.forEach(k => {
          html += `<span class="tag-badge xs danger">${escapeHtml(k)}: ${errs[k]}</span> `;
        });
        codesContainer.innerHTML = html;
      }
    }

    // Sparkline of recent latencies
    const sparkline = document.getElementById('lt-sparkline');
    if (sparkline && job.recent_latencies && job.recent_latencies.length > 0) {
      const maxL = Math.max(1, ...job.recent_latencies);
      sparkline.innerHTML = job.recent_latencies.map((l, idx) => {
        const h = Math.min(100, Math.max(8, (l / maxL) * 100));
        return `<div class="lt-spark-bar" style="height: ${h}%;" title="${l.toFixed(1)} ms"></div>`;
      }).join('');
    }
  }

  async function fetchHistory() {
    const tbody = document.getElementById('lt-history-body');
    if (!tbody) return;

    try {
      const res = await fetch('/api/loadtest/history');
      if (!res.ok) return;
      const data = await res.json();
      const history = data.history || [];

      if (history.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" class="table-empty">No previous benchmark runs.</td></tr>';
        return;
      }

      tbody.innerHTML = history.map((h, idx) => {
        const lat = h.latency || {};
        const successRate = h.total_sent > 0 ? ((h.total_success / h.total_sent) * 100).toFixed(1) : '0';
        return `
          <tr>
            <td class="mono font-bold">${escapeHtml(h.config?.name || h.id)}</td>
            <td><span class="tag-badge xs primary">${escapeHtml(h.config?.target_type)}</span></td>
            <td class="mono">${h.config?.concurrency} workers</td>
            <td class="mono">${h.total_sent} (${h.current_rps} rps)</td>
            <td class="mono font-bold ${h.total_failed > 0 ? 'text-danger' : 'text-success'}">${successRate}%</td>
            <td class="mono">${lat.avg_ms || 0} ms (p95: ${lat.p95_ms || 0}ms)</td>
            <td class="mono micro-label">${new Date(h.start_time).toLocaleTimeString()}</td>
          </tr>
        `;
      }).join('');
    } catch (e) {
      console.error('Failed to fetch load test history:', e);
    }
  }

  function toggleRunUI(running) {
    const btnStart = document.getElementById('btn-lt-start');
    const btnStop = document.getElementById('btn-lt-stop');
    const statusTag = document.getElementById('lt-cockpit-status');

    if (btnStart) btnStart.disabled = running;
    if (btnStop) btnStop.disabled = !running;

    if (statusTag) {
      statusTag.textContent = running ? 'BENCHMARK RUNNING' : 'COCKPIT IDLE';
      statusTag.className = `tag-badge ${running ? 'primary animate-pulse' : ''}`;
    }
  }

  function exportReportJSON() {
    if (!window.__latestLoadTestReport) {
      if (window.SSHPilot && window.SSHPilot.showToast) {
        window.SSHPilot.showToast('No benchmark report available to export', 'warning');
      }
      return;
    }
    const dataStr = "data:text/json;charset=utf-8," + encodeURIComponent(JSON.stringify(window.__latestLoadTestReport, null, 2));
    const dlAnchor = document.createElement('a');
    dlAnchor.setAttribute("href", dataStr);
    dlAnchor.setAttribute("download", `sshpilot_benchmark_${Date.now()}.json`);
    document.body.appendChild(dlAnchor);
    dlAnchor.click();
    dlAnchor.remove();
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  window.LoadTestController = {
    init,
    onActivated,
    onDeactivated
  };

  document.addEventListener('DOMContentLoaded', init);
})();
