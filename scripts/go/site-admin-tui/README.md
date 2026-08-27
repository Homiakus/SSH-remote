# Site Admin TUI

Server-side terminal application for deploying and operating websites on Ubuntu/Debian with `nginx`, `systemd`, atomic releases and a local registry of managed sites.

## What Is Implemented

- standalone Bubble Tea TUI with sections:
  - `Deploy Wizard`
  - `Sites`
  - `Execution Log`
  - `Doctor`
- real deploy engine for:
  - `static`
  - `php`
  - `node`
  - `python`
  - `docker_compose`
- atomic layout per site:
  - `/etc/site-admin-tui/config.yaml`
  - `/etc/site-admin-tui/sites/<name>.yaml`
  - `/var/lib/site-admin-tui/sites/<name>/releases/<release-id>`
  - `/var/lib/site-admin-tui/sites/<name>/current`
  - `/var/lib/site-admin-tui/sites/<name>/shared`
  - `/var/lib/site-admin-tui/sites/<name>/history.json`
  - `/var/lib/site-admin-tui/sites/<name>/lock`
- file-backed audit log in `/var/log/site-admin-tui/audit.log`
- deploy sources:
  - `git`
  - `existing_dir`
- operations:
  - preview plan
  - deploy
  - import existing directory into managed layout
  - restart
  - redeploy
  - rollback
  - doctor checks
- generated infrastructure:
  - per-site nginx config with `nginx -t` validation
  - systemd units for `node` and `python`
  - Certbot `webroot` TLS flow after successful activation
- safety features:
  - one lock per site
  - release history
  - nginx/systemd backups before rewrite
  - rollback when post-deploy health-check fails

## Runtime Defaults

- target OS: Ubuntu/Debian 22.04+
- expected user: `root`
- web server: `nginx`
- PHP service default: `php8.2-fpm`
- TLS: optional, via `certbot certonly --webroot`
- `node` default port: `3000`
- `python` default port: `8000`
- `docker_compose` default port: `8080`

## Commands

Run interactive TUI:

```bash
site-admin-tui
```

Run environment checks:

```bash
site-admin-tui doctor
```

Import an existing directory into the managed registry:

```bash
site-admin-tui import \
  --site demo \
  --path /srv/demo \
  --runtime static \
  --domain demo.example.com
```

Optional `import` flags:

- `--root-dir`
- `--port`
- `--start-command`
- `--compose-file`
- `--shared-dirs`
- `--env-file`
- `--tls`
- `--tls-email`

## TUI Deploy Wizard

The wizard currently collects:

- site name
- domain
- runtime
- source kind
- git repo / branch / subdir or existing directory
- root dir
- shared dirs
- env file
- runtime-specific fields:
  - `port`
  - `start command`
  - `compose file`
  - `php-fpm service`
- TLS toggle and email

Controls:

- `j/k` or arrow keys: move between fields
- `h/l` or arrow keys: change select field values
- typing: edit text field
- `p`: preview deploy plan
- `d`: execute deploy
- `esc`: back

## Build

Linux amd64:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o site-admin-tui ./scripts/go/site-admin-tui
```

Linux arm64:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o site-admin-tui ./scripts/go/site-admin-tui
```

## Install On Server

1. Copy the binary to the server, for example `/usr/local/bin/site-admin-tui`.
2. Ensure dependencies are installed:
   - `nginx`
   - `systemd`
   - `git`
   - `certbot`
   - runtime-specific tools such as `node`, `npm`, `python3`, `pip3`, `docker`
3. Run `site-admin-tui doctor`.
4. Fix missing requirements until all required checks are green.
5. Start the TUI and create/import the first site.

## Notes

- The application is intentionally independent from the current `SSHPilot` UI.
- Multi-server orchestration, monitoring dashboard, Caddy support and extended certificate management are still out of scope for this iteration.
