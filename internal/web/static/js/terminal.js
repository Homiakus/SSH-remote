/**
 * SSHPilot Terminal Engine
 * Full-featured VT100 / ANSI Terminal Emulator with virtual screen buffer,
 * 256/TrueColor support, cursor positioning, erase line/screen, and complete keyboard navigation.
 */

class SSHPilotTerminal {
  constructor(containerId) {
    this.container = document.getElementById(containerId);
    this.ws = null;
    this.activeServer = null;

    // Terminal geometry
    this.cols = 100;
    this.rows = 30;
    this.cursorX = 0;
    this.cursorY = 0;
    this.savedCursor = { x: 0, y: 0 };
    this.showCursor = true;

    // Screen buffer & styles
    this.lines = [];
    this.maxScrollback = 2000;
    this.currentFg = null;
    this.currentBg = null;
    this.currentBold = false;
    this.currentDim = false;
    this.currentUnderline = false;
    this.currentInverse = false;

    // Alternate screen buffer (vim, nano, htop, less)
    this.isAltScreen = false;
    this.altLines = [];
    this.savedMainLines = [];
    this.savedMainCursor = { x: 0, y: 0 };

    this.initDOM();
  }

  initDOM() {
    this.container.innerHTML = '';
    this.container.style.position = 'relative';

    // Terminal Screen Element
    this.termElem = document.createElement('div');
    this.termElem.className = 'sshpilot-ansi-term';
    this.termElem.style.cssText = `
      width: 100%;
      height: 100%;
      background: #060709;
      color: #e2e8f0;
      font-family: 'JetBrains Mono', 'SF Mono', 'Fira Code', Consolas, monospace;
      font-size: 13px;
      line-height: 1.35;
      padding: 10px 14px;
      overflow-y: auto;
      white-space: pre;
      outline: none;
      box-sizing: border-box;
      user-select: text;
      cursor: text;
    `;
    this.termElem.tabIndex = 0;

    // Hidden input helper for mobile virtual keyboards
    this.inputHelper = document.createElement('textarea');
    this.inputHelper.style.cssText = `
      position: absolute;
      top: -9999px;
      left: -9999px;
      width: 1px;
      height: 1px;
      opacity: 0;
      pointer-events: none;
    `;
    this.inputHelper.setAttribute('autocapitalize', 'off');
    this.inputHelper.setAttribute('autocomplete', 'off');
    this.inputHelper.setAttribute('autocorrect', 'off');
    this.inputHelper.setAttribute('spellcheck', 'false');

    this.container.appendChild(this.termElem);
    this.container.appendChild(this.inputHelper);

    // Focus bindings
    this.termElem.addEventListener('click', () => {
      this.termElem.focus();
    });

    // Keyboard Input
    this.termElem.addEventListener('keydown', (e) => this.handleKeyDown(e));
    this.inputHelper.addEventListener('keydown', (e) => this.handleKeyDown(e));

    // Global keydown routing when Console view is active
    window.addEventListener('keydown', (e) => {
      const consoleView = document.getElementById('view-console');
      if (!consoleView || !consoleView.classList.contains('active')) return;

      const active = document.activeElement;
      if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.tagName === 'SELECT')) {
        if (active !== this.inputHelper && active !== this.termElem) return;
      }

      // Check if modal is open
      const openModal = document.querySelector('.modal-overlay.active');
      if (openModal) return;

      if (active !== this.termElem) {
        this.termElem.focus();
        this.handleKeyDown(e);
      }
    });

    // Paste handling
    const handlePaste = (e) => {
      e.preventDefault();
      const text = (e.clipboardData || window.clipboardData).getData('text');
      if (text && this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(text);
      }
    };
    this.termElem.addEventListener('paste', handlePaste);

    // Resize listener
    let resizeTimer = null;
    window.addEventListener('resize', () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => this.handleResize(), 150);
    });

    this.resetScreen();
  }

  resetScreen() {
    this.lines = [];
    for (let i = 0; i < this.rows; i++) {
      this.lines.push([]);
    }
    this.cursorX = 0;
    this.cursorY = 0;
    this.resetAttributes();
    this.render();
  }

  resetAttributes() {
    this.currentFg = null;
    this.currentBg = null;
    this.currentBold = false;
    this.currentDim = false;
    this.currentUnderline = false;
    this.currentInverse = false;
  }

  connect(serverName) {
    if (!serverName) return;
    this.activeServer = serverName;
    if (this.ws) {
      try { this.ws.close(); } catch(e){}
    }

    this.resetScreen();
    this.write(`\x1b[33mConnecting to ${serverName} via SSH...\x1b[0m\r\n`);
    const statusText = document.getElementById('term-status-text');
    if (statusText) statusText.textContent = `CONNECTING: ${serverName}...`;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal?server=${encodeURIComponent(serverName)}&cols=${this.cols}&rows=${this.rows}`;
    
    this.ws = new WebSocket(wsUrl);
    this.ws.binaryType = 'arraybuffer';

    this.ws.onopen = () => {
      if (statusText) statusText.textContent = `CONNECTED: ${serverName}`;
      const dot = document.getElementById('server-status-dot');
      if (dot) dot.className = 'status-dot online';
      const activeName = document.getElementById('active-server-name');
      if (activeName) activeName.textContent = serverName.toUpperCase();
      this.termElem.focus();
      this.handleResize();
    };

    this.ws.onmessage = (event) => {
      let data = '';
      if (typeof event.data === 'string') {
        data = event.data;
      } else {
        const decoder = new TextDecoder('utf-8');
        data = decoder.decode(event.data);
      }
      this.write(data);
    };

    this.ws.onclose = () => {
      if (statusText) statusText.textContent = `DISCONNECTED`;
      const dot = document.getElementById('server-status-dot');
      if (dot) dot.className = 'status-dot';
      this.write(`\r\n\x1b[31m[SSHPilot] Session closed.\x1b[0m\r\n`);
    };

    this.ws.onerror = (err) => {
      this.write(`\r\n\x1b[31m[SSHPilot] WebSocket error: ${err.message || 'connection failed'}\x1b[0m\r\n`);
    };
  }

  handleKeyDown(e) {
    if (e.key === '/' && e.altKey) {
      if (window.SSHPilotSlashRunner) {
        window.SSHPilotSlashRunner.openPalette();
        e.preventDefault();
        return;
      }
    }

    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;

    let payload = '';

    // Function keys
    if (e.key === 'F1') payload = '\x1bOP';
    else if (e.key === 'F2') payload = '\x1bOQ';
    else if (e.key === 'F3') payload = '\x1bOR';
    else if (e.key === 'F4') payload = '\x1bOS';
    else if (e.key === 'F5') payload = '\x1b[15~';
    else if (e.key === 'F6') payload = '\x1b[17~';
    else if (e.key === 'F7') payload = '\x1b[18~';
    else if (e.key === 'F8') payload = '\x1b[19~';
    else if (e.key === 'F9') payload = '\x1b[20~';
    else if (e.key === 'F10') payload = '\x1b[21~';
    else if (e.key === 'F11') payload = '\x1b[23~';
    else if (e.key === 'F12') payload = '\x1b[24~';

    // Standard control navigation keys
    else if (e.key === 'Enter') payload = '\r';
    else if (e.key === 'Backspace') payload = '\x7f';
    else if (e.key === 'Tab') { e.preventDefault(); payload = '\t'; }
    else if (e.key === 'Escape') payload = '\x1b';
    else if (e.key === 'ArrowUp') payload = e.ctrlKey ? '\x1b[1;5A' : '\x1b[A';
    else if (e.key === 'ArrowDown') payload = e.ctrlKey ? '\x1b[1;5B' : '\x1b[B';
    else if (e.key === 'ArrowRight') payload = e.ctrlKey ? '\x1b[1;5C' : '\x1b[C';
    else if (e.key === 'ArrowLeft') payload = e.ctrlKey ? '\x1b[1;5D' : '\x1b[D';
    else if (e.key === 'Home') payload = '\x1b[H';
    else if (e.key === 'End') payload = '\x1b[F';
    else if (e.key === 'PageUp') payload = '\x1b[5~';
    else if (e.key === 'PageDown') payload = '\x1b[6~';
    else if (e.key === 'Insert') payload = '\x1b[2~';
    else if (e.key === 'Delete') payload = '\x1b[3~';

    // Ctrl combinations
    else if (e.ctrlKey && !e.altKey && !e.metaKey) {
      const keyUpper = e.key.toUpperCase();
      if (keyUpper.length === 1 && keyUpper >= 'A' && keyUpper <= 'Z') {
        payload = String.fromCharCode(keyUpper.charCodeAt(0) - 64);
      } else if (e.key === '@' || e.key === ' ') {
        payload = '\x00';
      } else if (e.key === '[') {
        payload = '\x1b';
      } else if (e.key === '\\') {
        payload = '\x1c';
      } else if (e.key === ']') {
        payload = '\x1d';
      } else if (e.key === '^' || e.key === '6') {
        payload = '\x1e';
      } else if (e.key === '_' || e.key === '/') {
        payload = '\x1f';
      }
    }

    // Alt combinations (ESC prefix)
    else if (e.altKey && !e.ctrlKey && !e.metaKey && e.key.length === 1) {
      payload = '\x1b' + e.key;
    }

    // Regular character typing
    else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
      payload = e.key;
    }

    if (payload) {
      this.ws.send(payload);
      e.preventDefault();
      e.stopPropagation();
    }
  }

  sendRaw(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data);
    }
  }

  handleResize() {
    if (!this.container) return;
    const width = this.container.clientWidth;
    const height = this.container.clientHeight;
    if (width <= 0 || height <= 0) return;

    const charWidth = 7.82;
    const charHeight = 17.55;
    const newCols = Math.max(40, Math.floor((width - 28) / charWidth));
    const newRows = Math.max(10, Math.floor((height - 20) / charHeight));

    if (newCols !== this.cols || newRows !== this.rows) {
      this.cols = newCols;
      this.rows = newRows;

      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({
          type: 'resize',
          cols: this.cols,
          rows: this.rows
        }));
      }
      this.render();
    }
  }

  clear() {
    this.resetScreen();
  }

  write(str) {
    let i = 0;
    const len = str.length;

    while (i < len) {
      const ch = str[i];

      // Carriage Return: \r (0x0D) -> Column 0
      if (ch === '\r') {
        this.cursorX = 0;
        i++;
        continue;
      }

      // Line Feed: \n (0x0A) or Form Feed \f or Vertical Tab \v -> Move down
      if (ch === '\n' || ch === '\x0b' || ch === '\x0c') {
        this.cursorY++;
        this.ensureLineExists(this.cursorY);
        i++;
        continue;
      }

      // Backspace: \b (0x08) -> Move left 1 char
      if (ch === '\b') {
        this.cursorX = Math.max(0, this.cursorX - 1);
        i++;
        continue;
      }

      // Tab: \t (0x09) -> Next 8-col tab stop
      if (ch === '\t') {
        this.cursorX = Math.min(this.cols - 1, (Math.floor(this.cursorX / 8) + 1) * 8);
        i++;
        continue;
      }

      // Bell: \x07 -> Ignore
      if (ch === '\x07') {
        i++;
        continue;
      }

      // Escape Sequences (\x1b)
      if (ch === '\x1b') {
        if (i + 1 < len) {
          const next = str[i + 1];

          // CSI Sequence: \x1b[ ...
          if (next === '[') {
            let j = i + 2;
            let params = '';
            while (j < len && !/[a-zA-Z~]/.test(str[j])) {
              params += str[j];
              j++;
            }
            if (j < len) {
              const cmd = str[j];
              this.handleCSI(cmd, params);
              i = j + 1;
              continue;
            }
          }

          // OSC Sequence: \x1b] ... (\x07 or \x1b\)
          else if (next === ']') {
            let j = i + 2;
            while (j < len && str[j] !== '\x07') {
              if (str[j] === '\x1b' && j + 1 < len && str[j + 1] === '\\') {
                j++;
                break;
              }
              j++;
            }
            i = j + 1;
            continue;
          }

          // Save / Restore Cursor: \x1b7, \x1b8
          else if (next === '7') {
            this.savedCursor = { x: this.cursorX, y: this.cursorY };
            i += 2;
            continue;
          } else if (next === '8') {
            this.cursorX = this.savedCursor.x;
            this.cursorY = this.savedCursor.y;
            i += 2;
            continue;
          }
        }
      }

      // Standard printable characters
      this.setCell(this.cursorY, this.cursorX, ch);
      this.cursorX++;
      if (this.cursorX >= this.cols) {
        this.cursorX = 0;
        this.cursorY++;
        this.ensureLineExists(this.cursorY);
      }
      i++;
    }

    this.render();
  }

  ensureLineExists(y) {
    while (this.lines.length <= y) {
      this.lines.push([]);
    }
    if (this.lines.length > this.maxScrollback) {
      const diff = this.lines.length - this.maxScrollback;
      this.lines.splice(0, diff);
      this.cursorY = Math.max(0, this.cursorY - diff);
    }
  }

  setCell(y, x, char) {
    this.ensureLineExists(y);
    const line = this.lines[y];
    while (line.length <= x) {
      line.push({ char: ' ', fg: null, bg: null, bold: false, dim: false, underline: false, inverse: false });
    }
    line[x] = {
      char: char,
      fg: this.currentFg,
      bg: this.currentBg,
      bold: this.currentBold,
      dim: this.currentDim,
      underline: this.currentUnderline,
      inverse: this.currentInverse
    };
  }

  handleCSI(cmd, paramStr) {
    const isPrivate = paramStr.startsWith('?');
    const cleanParams = isPrivate ? paramStr.slice(1) : paramStr;
    const parts = cleanParams ? cleanParams.split(';').map(n => parseInt(n, 10)) : [];
    const p1 = parts[0] !== undefined && !isNaN(parts[0]) ? parts[0] : 1;
    const p2 = parts[1] !== undefined && !isNaN(parts[1]) ? parts[1] : 1;

    switch (cmd) {
      case 'm':
        this.handleSGR(parts);
        break;
      case 'A':
        this.cursorY = Math.max(0, this.cursorY - p1);
        break;
      case 'B':
        this.cursorY = this.cursorY + p1;
        this.ensureLineExists(this.cursorY);
        break;
      case 'C':
        this.cursorX = Math.min(this.cols - 1, this.cursorX + p1);
        break;
      case 'D':
        this.cursorX = Math.max(0, this.cursorX - p1);
        break;
      case 'H':
      case 'f': {
        const targetRow = Math.max(0, p1 - 1);
        const targetCol = Math.max(0, p2 - 1);
        this.cursorY = targetRow;
        this.cursorX = Math.min(this.cols - 1, targetCol);
        this.ensureLineExists(this.cursorY);
        break;
      }
      case 'K': {
        const mode = parts[0] || 0;
        this.ensureLineExists(this.cursorY);
        const line = this.lines[this.cursorY];
        if (mode === 0) {
          line.length = Math.min(line.length, this.cursorX);
        } else if (mode === 1) {
          for (let c = 0; c <= Math.min(this.cursorX, line.length - 1); c++) {
            line[c] = { char: ' ', fg: null, bg: null };
          }
        } else if (mode === 2) {
          line.length = 0;
        }
        break;
      }
      case 'J': {
        const mode = parts[0] || 0;
        if (mode === 0) {
          if (this.lines[this.cursorY]) {
            this.lines[this.cursorY].length = Math.min(this.lines[this.cursorY].length, this.cursorX);
          }
          this.lines.length = this.cursorY + 1;
        } else if (mode === 2 || mode === 3) {
          this.lines = [];
          for (let r = 0; r < this.rows; r++) this.lines.push([]);
          this.cursorX = 0;
          this.cursorY = 0;
        }
        break;
      }
      case 'h':
        if (isPrivate) {
          if (p1 === 25) this.showCursor = true;
          if (p1 === 1049 || p1 === 47) {
            this.isAltScreen = true;
            this.savedMainLines = this.lines;
            this.savedMainCursor = { x: this.cursorX, y: this.cursorY };
            this.lines = [];
            for (let r = 0; r < this.rows; r++) this.lines.push([]);
            this.cursorX = 0;
            this.cursorY = 0;
          }
        }
        break;
      case 'l':
        if (isPrivate) {
          if (p1 === 25) this.showCursor = false;
          if (p1 === 1049 || p1 === 47) {
            this.isAltScreen = false;
            this.lines = this.savedMainLines;
            this.cursorX = this.savedMainCursor.x;
            this.cursorY = this.savedMainCursor.y;
          }
        }
        break;
      case 'G':
        this.cursorX = Math.max(0, Math.min(this.cols - 1, p1 - 1));
        break;
      case 'P': {
        this.ensureLineExists(this.cursorY);
        const line = this.lines[this.cursorY];
        line.splice(this.cursorX, p1);
        break;
      }
      case '@': {
        this.ensureLineExists(this.cursorY);
        const line = this.lines[this.cursorY];
        for (let b = 0; b < p1; b++) {
          line.splice(this.cursorX, 0, { char: ' ', fg: null, bg: null });
        }
        break;
      }
    }
  }

  handleSGR(parts) {
    if (parts.length === 0) {
      this.resetAttributes();
      return;
    }

    let i = 0;
    while (i < parts.length) {
      const code = parts[i];
      if (isNaN(code) || code === 0) {
        this.resetAttributes();
      } else if (code === 1) {
        this.currentBold = true;
      } else if (code === 2) {
        this.currentDim = true;
      } else if (code === 4) {
        this.currentUnderline = true;
      } else if (code === 7) {
        this.currentInverse = true;
      } else if (code === 22) {
        this.currentBold = false;
        this.currentDim = false;
      } else if (code === 24) {
        this.currentUnderline = false;
      } else if (code === 27) {
        this.currentInverse = false;
      }

      // Standard Foreground Colors (30-37, 90-97)
      else if (code >= 30 && code <= 37) {
        const colors = ['#1f2937', '#ef4444', '#22c55e', '#eab308', '#3b82f6', '#a855f7', '#06b6d4', '#e2e8f0'];
        this.currentFg = colors[code - 30];
      } else if (code === 39) {
        this.currentFg = null;
      } else if (code >= 90 && code <= 97) {
        const bright = ['#4b5563', '#f87171', '#4ade80', '#fde047', '#60a5fa', '#c084fc', '#22d3ee', '#ffffff'];
        this.currentFg = bright[code - 90];
      }

      // Standard Background Colors (40-47, 100-107)
      else if (code >= 40 && code <= 47) {
        const bgColors = ['#000000', '#7f1d1d', '#14532d', '#713f12', '#1e3a8a', '#581c87', '#164e63', '#475569'];
        this.currentBg = bgColors[code - 40];
      } else if (code === 49) {
        this.currentBg = null;
      } else if (code >= 100 && code <= 107) {
        const brightBg = ['#374151', '#991b1b', '#166534', '#854d0e', '#1e40af', '#6b21a8', '#155e75', '#64748b'];
        this.currentBg = brightBg[code - 100];
      }

      // 256 / TrueColor Extended Palette (38;5;N or 38;2;R;G;B)
      else if (code === 38 && i + 2 < parts.length) {
        if (parts[i + 1] === 5) {
          this.currentFg = this.ansi256ToColor(parts[i + 2]);
          i += 2;
        } else if (parts[i + 1] === 2 && i + 4 < parts.length) {
          this.currentFg = `rgb(${parts[i + 2]},${parts[i + 3]},${parts[i + 4]})`;
          i += 4;
        }
      } else if (code === 48 && i + 2 < parts.length) {
        if (parts[i + 1] === 5) {
          this.currentBg = this.ansi256ToColor(parts[i + 2]);
          i += 2;
        } else if (parts[i + 1] === 2 && i + 4 < parts.length) {
          this.currentBg = `rgb(${parts[i + 2]},${parts[i + 3]},${parts[i + 4]})`;
          i += 4;
        }
      }

      i++;
    }
  }

  ansi256ToColor(n) {
    if (n < 8) {
      const std = ['#000000', '#ef4444', '#22c55e', '#eab308', '#3b82f6', '#a855f7', '#06b6d4', '#e2e8f0'];
      return std[n] || '#e2e8f0';
    }
    if (n < 16) {
      const bright = ['#4b5563', '#f87171', '#4ade80', '#fde047', '#60a5fa', '#c084fc', '#22d3ee', '#ffffff'];
      return bright[n - 8] || '#ffffff';
    }
    if (n >= 232) {
      const gray = Math.round((n - 232) * 10 + 8);
      return `rgb(${gray},${gray},${gray})`;
    }
    const val = n - 16;
    const r = Math.floor(val / 36) * 51;
    const g = Math.floor((val % 36) / 6) * 51;
    const b = (val % 6) * 51;
    return `rgb(${r},${g},${b})`;
  }

  // --- Rendering to DOM with Live Cursor ---
  render() {
    const htmlParts = [];
    const totalLines = this.lines.length;

    for (let y = 0; y < totalLines; y++) {
      const line = this.lines[y] || [];
      let lineHtml = '';
      const lineLen = Math.max(line.length, y === this.cursorY ? this.cursorX + 1 : 0);

      for (let x = 0; x < lineLen; x++) {
        const isCursor = this.showCursor && y === this.cursorY && x === this.cursorX;
        const cell = line[x] || { char: ' ', fg: null, bg: null };
        const char = cell.char || ' ';

        let style = '';
        let fg = cell.fg;
        let bg = cell.bg;

        if (cell.inverse) {
          const tmp = fg || '#e2e8f0';
          fg = bg || '#060709';
          bg = tmp;
        }

        if (fg) style += `color:${fg};`;
        if (bg) style += `background-color:${bg};`;
        if (cell.bold) style += 'font-weight:700;';
        if (cell.dim) style += 'opacity:0.6;';
        if (cell.underline) style += 'text-decoration:underline;';

        const safeChar = char === '&' ? '&amp;' : (char === '<' ? '&lt;' : (char === '>' ? '&gt;' : (char === ' ' ? '&nbsp;' : char)));

        if (isCursor) {
          lineHtml += `<span class="term-cursor" style="${style}background:var(--accent,#d9f927);color:#000;font-weight:700;">${safeChar}</span>`;
        } else if (style) {
          lineHtml += `<span style="${style}">${safeChar}</span>`;
        } else {
          lineHtml += safeChar;
        }
      }

      if (this.showCursor && y === this.cursorY && this.cursorX >= lineLen) {
        lineHtml += `<span class="term-cursor" style="background:var(--accent,#d9f927);color:#000;font-weight:700;">&nbsp;</span>`;
      }

      htmlParts.push(lineHtml || '&nbsp;');
    }

    this.termElem.innerHTML = htmlParts.join('\n');
    this.termElem.scrollTop = this.termElem.scrollHeight;
  }
}

window.SSHPilotTerminal = SSHPilotTerminal;
