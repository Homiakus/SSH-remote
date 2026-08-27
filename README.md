# SSHPILOT // Next-Gen Fleet Control Plane & Terminal Hub

[![Go Report Card](https://goreportcard.com/badge/sshpilot)](https://goreportcard.com)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20darwin-lightgrey)](https://github.com)
[![Architecture](https://img.shields.io/badge/arch-amd64%20%7C%20arm64-blue)](https://github.com)

**SSHPilot** is a high-performance, single-binary SSH management suite and remote infrastructure control plane. It features both an interactive **Charm Bubble Tea TUI** and a modern **Neo-Swiss Web UI** with real-time WebSocket PTY emulation, SFTP file management, SSH key vault, and multi-stage script/payload deployment pipelines.

---

## ⚡ Key Capabilities

### 🌐 Neo-Swiss Responsive Web Control Plane
- **Fluid & Intrinsic System**: Continuous scaling via `clamp()`, container queries, and sub-pixel alignment without rigid layout breaks.
- **Zero Horizontal Overflow**: Verified `scrollWidth === clientWidth` from ultra-compact smartphones (`320px`) to curved ultrawide monitors (`3840px`).
- **Dynamic Viewports & Safe Areas**: Native-grade mobile ergonomics respecting `100dvh`/`100svh` and `env(safe-area-inset-*)` notches and home bars.
- **Mobile Bottom Navigation & Off-Canvas Drawer**: Quick one-thumb switching between primary views on screens `<= 960px`.
- **Mobile Touch Terminal Accessory Bar**: Virtual action keys (`ESC`, `TAB`, `^C`, `^Z`, `/`, `▲`, `▼`, `◀`, `▶`) sending raw ANSI codes directly to the remote PTY.
- **Adaptive Data Views**: Seamless transformation of multi-column server/vault tables into stacked mobile cards and slide-up bottom sheets with gesture handles.
- **Theme & Accent Architecture**: Zero-FOUC theme engine with Light / Dark modes and curated Swiss design accents (Neon Lime, Electric Amber, Hyper Cyan, Swiss Signal Red, Neutral Mono).

### 💻 Dual Interface Architecture
- **Web UI (`main.go --port 8080`)**: Zero external JavaScript runtime dependencies. Embedded static assets (`//go:embed`) with real-time WebSocket communication.
- **Terminal TUI (`main.go --tui`)**: Built with the Charm ecosystem (`bubbletea`, `lipgloss`, `bubbles`) for headless servers, bastions, and local command-line power users.

### 🛡️ Fleet Management & Remote Tooling
- **Interactive Terminal Session**: Full VT100 / xterm-256color ANSI emulation with dynamic window resizing and bidirectional streaming over WebSockets.
- **SFTP Remote Explorer & Inline Editor**: Directory navigation, permission inspection (`drwxr-xr-x`), file downloads, and code/config editing with instant remote sync.
- **SSH Key Vault**: Management of Ed25519, RSA, and ECDSA keypairs with passphrases and key testing.
- **Command Runner & Deployment Pipeline (`/` shortcut)**: 4-stage deployment orchestration (GitHub commit hash verification, payload staging, SFTP distribution, and remote execution).

---

## 📐 Responsive QA Matrix

| Viewport | Device Class | Mode | Overflow Status | Notes |
| :--- | :--- | :--- | :---: | :--- |
| **320 × 568** | Extra Compact (iPhone SE 1st Gen) | Portrait | **0px (PASS)** | Uncluttered header, stacked cards, bottom bar |
| **390 × 844** | Standard Phone (iPhone 14/15) | Portrait | **0px (PASS)** | Touch key accessory bar, bottom sheets |
| **844 × 390** | Mobile Landscape | Landscape | **0px (PASS)** | Safe-area aware, compact headers |
| **768 × 1024** | Tablet Portrait (iPad) | Portrait | **0px (PASS)** | Container query card distribution |
| **1024 × 768** | Tablet Landscape | Landscape | **0px (PASS)** | Full desktop navigation bar on single row |
| **1366 × 768** | Standard Laptop | Landscape | **0px (PASS)** | 6-column list rows with aligned action buttons |
| **1920 × 1080** | Full HD Desktop | Landscape | **0px (PASS)** | Swiss editorial typography and grid rhythm |
| **3440 × 1440** | Curved Ultrawide | Landscape | **0px (PASS)** | Centered max-width containment |

---

## 🚀 Quick Start

### Prerequisites
- [Go 1.22+](https://go.dev/dl/) installed.

### Run Directly
```bash
# Clone the repository
git clone https://github.com/Homiakus/SSH-remote.git
cd SSH-remote

# Run Web Control Plane (Default on http://127.0.0.1:8080)
go run main.go

# Or run in headless TUI mode
go run main.go --tui
```

### CLI Flags
```text
Usage of sshpilot:
  --host string      Web server host (default "127.0.0.1")
  --port string      Web server port (default "8080")
  --no-browser       Do not automatically open browser on startup
  --tui              Launch legacy Charm Bubble Tea TUI interface
```

---

## 🔨 Building & Cross-Compilation

Use the built-in PowerShell build system `build.ps1` for cross-platform binaries:

```powershell
# Build for current operating system & architecture
.\build.ps1 -Target current

# Build for all supported platforms (Windows, Linux, macOS - x64 & ARM64)
.\build.ps1 -Target all
```

Or build manually using standard Go tooling:
```bash
# Build standalone binary
go build -trimpath -ldflags="-s -w" -o sshpilot.exe .
```

All compiled binaries are placed into the `dist/` directory.

---

## ⌨️ Global Keyboard Shortcuts

| Shortcut | Scope | Action |
| :--- | :--- | :--- |
| `/` | Global | Open Quick Command Runner & Deployment Palette |
| `ESC` | Modals / Palette | Close active dialog / modal bottom sheet |
| `Ctrl + C` | Terminal | Send SIGINT interrupt to remote process |
| `Ctrl + Z` | Terminal | Send SIGTSTP suspend signal |
| `TAB` | Terminal | Trigger remote shell auto-completion |

---

## 📡 REST API Reference

| Endpoint | Method | Description |
| :--- | :---: | :--- |
| `/api/servers` | `GET`, `POST` | List configured server nodes / Add new node |
| `/api/servers/{name}` | `GET`, `DELETE` | Inspect node telemetry / Remove node |
| `/api/servers/{name}/test` | `POST` | Execute live SSH handshake and latency ping |
| `/api/keys` | `GET`, `POST` | List SSH key vault / Register new keypair |
| `/api/fs/list?server={name}&path={p}` | `GET` | Retrieve SFTP directory contents |
| `/api/fs/read?server={name}&path={p}` | `GET` | Read remote file content |
| `/api/fs/write` | `POST` | Save edited file content to remote server |
| `/api/scripts` | `GET`, `POST` | Manage runnable automation scripts |
| `/api/scripts/execute` | `POST` | Trigger multi-stage execution pipeline |
| `/ws/terminal` | `WS` | Real-time bidirectional PTY stream |

---

## 📂 Project Architecture

```text
ssh-console/
├── main.go                     # Application entry point & CLI flags
├── build.ps1                   # Cross-platform multi-arch build automation
├── internal/
│   ├── config/                 # Host configurations, serialization & storage
│   ├── log/                    # Audit logging & trace collection
│   ├── scripts/                # Automated script executor & pipeline stages
│   ├── ssh/                    # SSH client, PTY allocators & SFTP subsystem
│   ├── ui/                     # Charm Bubble Tea TUI components & themes
│   └── web/                    # HTTP router, API handlers & WebSocket hub
│       ├── handlers/           # REST endpoints & PTY websocket handlers
│       └── static/             # Embedded HTML/CSS/JS web assets
│           ├── index.html      # Responsive Single-Page Application
│           ├── css/app.css     # Fluid Neo-Swiss design system
│           └── js/             # Modular vanilla controllers
└── scripts/                    # Helper utilities & site-admin automation
```

---

## 📄 License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
