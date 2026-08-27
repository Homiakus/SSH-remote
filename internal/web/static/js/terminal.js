/**
 * SSHPilot Terminal Engine
 * High-performance WebSocket Terminal with ANSI decoder & window resizing.
 */

class SSHPilotTerminal {
  constructor(containerId) {
    this.container = document.getElementById(containerId);
    this.ws = null;
    this.activeServer = null;
    this.cols = 100;
    this.rows = 32;
    this.cursorX = 0;
    this.cursorY = 0;
    this.lines = [];
    this.maxLines = 1000;
    this.initDOM();
  }

  initDOM() {
    this.container.innerHTML = '';
    this.termElem = document.createElement('div');
    this.termElem.className = 'sshpilot-ansi-term';
    this.termElem.style.cssText = `
      width: 100%;
      height: 100%;
      background: #000;
      color: #f3f4f6;
      font-family: 'JetBrains Mono', 'SF Mono', monospace;
      font-size: 13px;
      line-height: 1.35;
      padding: 8px 12px;
      overflow-y: auto;
      white-space: pre-wrap;
      word-break: break-all;
      outline: none;
      box-sizing: border-box;
    `;
    this.termElem.tabIndex = 0;
    this.container.appendChild(this.termElem);

    // Keyboard Input
    this.termElem.addEventListener('keydown', (e) => this.handleKeyDown(e));
    this.termElem.addEventListener('paste', (e) => {
      e.preventDefault();
      const text = (e.clipboardData || window.clipboardData).getData('text');
      if (text && this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(text);
      }
    });

    // Resize listener
    window.addEventListener('resize', () => this.handleResize());
  }

  connect(serverName) {
    if (!serverName) return;
    this.activeServer = serverName;
    if (this.ws) {
      try { this.ws.close(); } catch(e){}
    }

    this.termElem.innerHTML = '';
    this.appendOutput(`\x1b[33mConnecting to ${serverName} via SSH...\x1b[0m\r\n`);
    document.getElementById('term-status-text').textContent = `CONNECTING: ${serverName}...`;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal?server=${encodeURIComponent(serverName)}&cols=${this.cols}&rows=${this.rows}`;
    
    this.ws = new WebSocket(wsUrl);
    this.ws.binaryType = 'arraybuffer';

    this.ws.onopen = () => {
      document.getElementById('term-status-text').textContent = `CONNECTED: ${serverName}`;
      document.getElementById('server-status-dot').className = 'status-dot online';
      document.getElementById('active-server-name').textContent = serverName.toUpperCase();
      this.termElem.focus();
    };

    this.ws.onmessage = (event) => {
      let data = '';
      if (typeof event.data === 'string') {
        data = event.data;
      } else {
        const decoder = new TextDecoder('utf-8');
        data = decoder.decode(event.data);
      }
      this.appendOutput(data);
    };

    this.ws.onclose = () => {
      document.getElementById('term-status-text').textContent = `DISCONNECTED`;
      document.getElementById('server-status-dot').className = 'status-dot';
      this.appendOutput(`\r\n\x1b[31m[SSHPilot] Session closed.\x1b[0m\r\n`);
    };

    this.ws.onerror = (err) => {
      this.appendOutput(`\r\n\x1b[31m[SSHPilot] WebSocket error: ${err.message || 'connection failed'}\x1b[0m\r\n`);
    };
  }

  handleKeyDown(e) {
    // Intercept '/' key if Alt or standalone to trigger Slash Command Palette
    if (e.key === '/' && !e.ctrlKey && !e.metaKey && this.termElem.textContent.endsWith('$ ') || (e.key === '/' && e.altKey)) {
      // Trigger slash palette
      if (window.SSHPilotSlashRunner) {
        window.SSHPilotSlashRunner.openPalette();
        e.preventDefault();
        return;
      }
    }

    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;

    let sendKey = '';
    if (e.key === 'Enter') sendKey = '\r';
    else if (e.key === 'Backspace') sendKey = '\x7f';
    else if (e.key === 'Tab') { e.preventDefault(); sendKey = '\t'; }
    else if (e.key === 'Escape') sendKey = '\x1b';
    else if (e.key === 'ArrowUp') sendKey = '\x1b[A';
    else if (e.key === 'ArrowDown') sendKey = '\x1b[B';
    else if (e.key === 'ArrowRight') sendKey = '\x1b[C';
    else if (e.key === 'ArrowLeft') sendKey = '\x1b[D';
    else if (e.ctrlKey && e.key.length === 1) {
      const code = e.key.toUpperCase().charCodeAt(0);
      if (code >= 65 && code <= 90) {
        sendKey = String.fromCharCode(code - 64);
      }
    } else if (e.key.length === 1 && !e.altKey && !e.ctrlKey && !e.metaKey) {
      sendKey = e.key;
    }

    if (sendKey) {
      this.ws.send(sendKey);
      e.preventDefault();
    }
  }

  handleResize() {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const width = this.container.clientWidth;
    const height = this.container.clientHeight;
    const charWidth = 8.2;
    const charHeight = 18;
    this.cols = Math.max(40, Math.floor((width - 24) / charWidth));
    this.rows = Math.max(10, Math.floor((height - 16) / charHeight));

    this.ws.send(JSON.stringify({
      type: 'resize',
      cols: this.cols,
      rows: this.rows
    }));
  }

  sendRaw(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data);
    }
  }

  clear() {
    this.termElem.innerHTML = '';
  }

  appendOutput(raw) {
    const formatted = this.ansiToHtml(raw);
    this.termElem.innerHTML += formatted;
    this.termElem.scrollTop = this.termElem.scrollHeight;
  }

  ansiToHtml(text) {
    return text
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/\x1b\[0?m/g, '</span>')
      .replace(/\x1b\[1m/g, '<span style="font-weight:bold">')
      .replace(/\x1b\[2m/g, '<span style="opacity:0.6">')
      .replace(/\x1b\[30m/g, '<span style="color:#4b5563">')
      .replace(/\x1b\[31m/g, '<span style="color:#ef4444">')
      .replace(/\x1b\[32m/g, '<span style="color:#22c55e">')
      .replace(/\x1b\[33m/g, '<span style="color:#eab308">')
      .replace(/\x1b\[34m/g, '<span style="color:#3b82f6">')
      .replace(/\x1b\[35m/g, '<span style="color:#a855f7">')
      .replace(/\x1b\[36m/g, '<span style="color:#06b6d4">')
      .replace(/\x1b\[37m/g, '<span style="color:#f3f4f6">')
      .replace(/\x1b\[[0-9;]*[a-zA-Z]/g, ''); // strip remaining control codes
  }
}

window.SSHPilotTerminal = SSHPilotTerminal;
