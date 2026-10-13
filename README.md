<p align="center"><img src="assets/brand/ducktainer-logo.svg" alt="ducktainer: containerized applications with a squeak" width="420"></p>

# ducktainer

[![CI](https://github.com/leandrofus/ducktainer/actions/workflows/ci.yml/badge.svg)](https://github.com/leandrofus/ducktainer/actions/workflows/ci.yml)

**Agentless Docker management in a single binary.** A CRM-style web UI (right-click everything, saved views, kanban, ⌘K palette), compose stacks with a visual editor, an app marketplace, live logs and web terminals. Sign-in uses your **system users** (PAM), and network access is limited to the hosts you allow.

[Español](README.es.md) · [ducktainer website](https://leandrofus.github.io/ducktainer/)

- **Agentless**: remote hosts are reached over **SSH** (`docker system dial-stdio`, like `DOCKER_HOST=ssh://`) or through `ducktainer expose`. Nothing but Docker is needed on them.
- **Least privilege**: the service runs as an unprivileged user. `ducktainer setup` adapts it to however Docker is installed: socket group, rootless or root-only. Only password checks run as root, in a socket-activated helper.
- **Access control**: an IP/CIDR/hostname allowlist (connections are dropped before TLS), roles from POSIX groups, automatic TLS with certificate pinning, and an audit log.
- **Languages**: English and Spanish. The UI follows the browser and can be switched from the user menu; the CLI follows `LANG`.

---

## Getting started

### 1. Install

**Packages (recommended)**: RHEL/Alma/Rocky 9+, Fedora, Debian 12+, Ubuntu 22.04+ (amd64 and arm64). Docker must be installed (the package depends on it).

```sh
sudo dnf install ./ducktainer-0.1.0-1.x86_64.rpm     # RHEL / Fedora
sudo apt install ./ducktainer_0.1.0-1_amd64.deb      # Debian / Ubuntu
```

The package:

- creates the `ducktainer` user and the `ducktainer-admins` / `ducktainer-viewers` groups;
- runs `ducktainer setup` to detect how Docker is installed;
- enables the `ducktainer` service, listening on `http://127.0.0.1:9900`.

**Single binary** (any Linux, macOS or Windows; token sign-in only, no PAM):

```sh
./ducktainer serve            # http://127.0.0.1:9900, prints the access token
./ducktainer ui               # same, and opens the browser
```

**Windows without Docker Desktop.** `ducktainer engine` installs the open-source Docker Engine for you, from an administrator terminal:

```powershell
.\ducktainer.exe engine install                 # Linux containers: Docker in a WSL 2 distribution of its own
.\ducktainer.exe engine install --mode native   # Windows containers: dockerd.exe as a Windows service
.\ducktainer.exe engine status
.\ducktainer.exe ui
```

- `wsl` (the default) creates the `ducktainer` WSL 2 distribution from Ubuntu's image (checked against Ubuntu's SHA256SUMS), runs `docker.io` there with systemd and adds the `wsl` host (`wsl://ducktainer`). A logon task keeps the distribution running, so containers don't stop when WSL would shut it down. WSL distributions belong to a user: run ducktainer as that user.
- `native` enables the Containers feature (and Hyper-V on Windows 10/11), installs `dockerd.exe`, `docker.exe` and the compose plugin in `C:\Program Files\Docker`, registers the `docker` service and points the `local` host at `npipe:////./pipe/docker_engine`. Windows Home has no Containers feature: use `wsl`.
- If Windows needs a restart (to enable WSL or a feature), restart and run the same command again.

### 2. Check how ducktainer reaches Docker

```sh
sudo ducktainer setup --dry-run
```

```
socket:  /var/run/docker.sock  (root:docker 0660, standard path)
mode:    group
  · access through group docker (socket root:docker 0660)
```

| Detected installation | The service runs as |
|---|---|
| Socket accessible by a group (`root:docker 0660`, any group name) | `ducktainer` + that group |
| Rootless Docker (`/run/user/1000/docker.sock`) | the socket owner |
| Root-only socket, Podman | root (with a warning) |
| Installed but not started yet | what the `docker.socket` unit says |

Run `sudo ducktainer setup` again whenever the Docker installation changes.

### 3. Give people access

Accounts are ordinary system users, local or from LDAP/SSSD/FreeIPA. Their **role** depends on their groups:

| Role | Groups (default) | Can |
|---|---|---|
| **Administrator** | `ducktainer-admins`, `docker` | everything: containers, stacks, images, terminals, settings, users |
| **Read-only** | `ducktainer-viewers` | see everything except settings; cannot change anything, open terminals or download files |

```sh
sudo useradd -m ana && sudo passwd ana       # only if the account doesn't exist yet
sudo usermod -aG ducktainer-admins ana          # administrator  (or ducktainer-viewers: read-only)
sudo ducktainer auth test ana                   # checks the password and shows the resulting role
```

- Group membership is re-checked every 5 minutes, so removing someone from the group revokes their access quickly.
- `root` cannot sign in unless you enable `allow_root`.
- The UI shows all of this under **Settings → Access → Who can sign in**: the role → group mapping, the current members of each group, and a guide that writes these commands for you.

### 4. Open it to the network

By default ducktainer only listens on the local machine. Allow the addresses that may connect (IPs, CIDR ranges or host names; host names are re-resolved every minute) and restart:

```sh
sudo ducktainer allow add 192.168.1.0/24 office.example.com
sudo systemctl restart ducktainer       # now it listens on 0.0.0.0:9900, with TLS
```

- Anyone else is disconnected before TLS. The local machine is always allowed.
- **Settings → Access → From where** shows the listen address, your own IP and the rule that lets it in. You can edit the allowlist there; changes apply immediately. There is also a "would this address be allowed?" checker.
- The TLS certificate is self-signed. Its SHA-256 fingerprint is printed at startup so clients can pin it. Use your own certificate with `--cert/--key` in `/etc/ducktainer/ducktainer.env`.

### 5. Sign in

Open `https://<server>:9900` and sign in with your system user and password.

The **emergency token** signs in as administrator and authorizes the API (`Authorization: Bearer`). Only its hash is stored, so it is shown once: `sudo ducktainer token --rotate` creates and prints a new one. To allow only system users, disable it with `sudo ducktainer auth set --token-login off`.

### 6. Add more Docker hosts (optional)

```sh
# using the outbound agent (ideal for nodes behind NAT/firewall): add the host
# first (Settings → Hosts → Agent, or the line below), which gives it a token of
# its own, then run on the node the command shown in "Copy agent install command":
ducktainer endpoint add worker-1 agent://          # prints the host's token
#   curl -fsSL -k --pinnedpubkey 'sha256//…' -H 'Authorization: Bearer <host token>' \
#     'https://<ducktainer-server>:9900/api/agent/install.sh?server=…' | sudo sh

# over SSH: nothing to install on the remote (key-based access; the user must be allowed to use Docker)
sudo -u ducktainer ssh-keygen -t ed25519          # key in /var/lib/ducktainer/.ssh, add it to the remote's authorized_keys
ducktainer endpoint add prod ssh://deploy@prod.example.com

# or through the proxy, on the remote host:
ducktainer expose --allow <ducktainer-server-ip>   # prints the exact `ducktainer endpoint add …` line to run here
```

Hosts can also be managed from **Settings → Hosts**, and you switch between them from the top of the sidebar.

- Each agent host has its own token, valid only for that host; the admin token is not accepted from agents. The install command sends it in a header, pins this server's certificate (no `--insecure`) and leaves it on the node in `/etc/ducktainer-agent/agent.env` (root only). Hosts registered by earlier versions with the admin token are refused until you give them a token in Settings → Hosts and reinstall the agent.
- `tcp://` hosts must use TLS unless they are on the same machine; without `--fingerprint`, the certificate presented when the host is added is pinned.
- SSH hosts record the host key on the first connection (`StrictHostKeyChecking=accept-new`); a changed key is refused.
- Registry credentials can be limited to some hosts (`--hosts`, or in the UI): compose runs on a node get a copy of the credentials they are allowed to see.

#### Trust model

ducktainer controls the Docker API of every host, which is equivalent to root on them: whoever controls the ducktainer server (or its admin accounts) controls all hosts. Keep that machine and `/etc/ducktainer/secret.key` as protected as the hosts themselves. To edit stacks on a node, ducktainer starts a helper container there (`ducktainer-files`) that mounts the node's `/`; it exits after an hour idle.

---

## Features

- **Containers**: a table grouped by stack, or a kanban by state (drag a card to "Stopped" to stop it), saved views, bulk actions and quick view. Each record has Overview (live CPU/memory/network plus activity), Logs, Terminal, Files, Performance, Processes, Configuration and Inspect.
- **Editing containers**: Docker can't modify a container in place, so ducktainer creates a new one with the edited settings, swaps the names and removes the old one. Volumes are kept, and the original is restored if the new one fails to start. Containers can also be duplicated, updated (pull + recreate) and committed to an image.
- **Compose stacks**:
  - a **visual editor** (YAML stays the source of truth: comments, formatting and variables are preserved) and a YAML editor;
  - multi-file projects (base + overrides) with a merged view and "merge into one file";
  - validation before saving (`docker compose config`), deploy and update, combined logs per service;
  - stacks created outside ducktainer are detected and edited in place.
- **Images**: pull and push with per-layer progress, tag, build from a Dockerfile, layer history, cleanup.
- **Networks**: a registry with connected containers and their IPs, a connection map, create, connect and disconnect.
- **Volumes**: who uses each one, orphans, sizes, and browsing their contents.
- **Marketplace**: about 26 built-in apps plus any Portainer v2/v3 template list (the official and community ~800-app lists are preconfigured).
- **Audit log**: every sign-in and every write or terminal action, in Settings → Audit and in `journalctl -u ducktainer`.

## Command line

| Command | Purpose |
|---|---|
| `ducktainer serve` | Web UI + API (`--listen`, `--allow`, `--tls auto\|on\|off`, `--cert/--key`, `--expose :2376`, `--agent-listen :9901`) |
| `ducktainer agent` | Outbound agent (`--server`, `--name`, `--fingerprint`, `--socket`, `--install-service`; token in `DUCKTAINER_AGENT_TOKEN`) |
| `ducktainer expose` | Publish only the Docker API, protected (allowlist + token + TLS; `--read-only`, `--no-exec`) |
| `ducktainer engine install [--mode wsl\|native] \| status` | Windows: install Docker without Docker Desktop |
| `ducktainer setup [--dry-run]` | Adapt the service to the Docker installation |
| `ducktainer auth show \| set … \| test <user>` | System-user sign-in and role mapping |
| `ducktainer allow ls \| add \| rm` | Network allowlist |
| `ducktainer token --rotate` | New emergency/API token (only its hash is stored) |
| `ducktainer endpoint ls \| add \| rm` | Docker hosts (`unix://`, `ssh://user@host`, `tcp://host:2376`, `agent://name`) |
| `ducktainer registry ls \| add \| rm` | Private registry credentials (`--hosts` limits which hosts get them) |
| `ducktainer ps \| logs \| sh` | Containers by stack · logs of a container or a whole stack · interactive shell (picks bash/ash/sh) |
| `ducktainer docker-env <host>` | `eval "$(ducktainer docker-env prod)"` to use the docker CLI against a host |

## Files

| Path | Contents |
|---|---|
| `/etc/ducktainer/ducktainer.env` | `DUCKTAINER_OPTS` for `ducktainer serve` |
| `/etc/pam.d/ducktainer` | PAM stack for sign-in (`password-auth` on RHEL, `common-auth` on Debian) |
| `/var/lib/ducktainer/` | configuration, stacks, audit log, TLS certificate, the service's SSH keys |
| `/etc/systemd/system/ducktainer.service.d/10-docker-access.conf` | written by `ducktainer setup` |
| `/etc/ducktainer/secret.key` | key that encrypts the secrets in the configuration (root-only, passed to the service with systemd `LoadCredential`) |

Secrets in `config.json` (session secret, host tokens, registry passwords) are encrypted with AES-256-GCM, and the emergency token is stored only as a SHA-256 hash. The key is kept apart from the data, so a copy or backup of `/var/lib/ducktainer` reveals nothing without it. Back up `/etc/ducktainer/secret.key` separately: without it the configuration cannot be read. For a binary run without the package, the key is `secret.key` in the data directory, unless `DUCKTAINER_KEY_FILE` points elsewhere. Plaintext configurations from earlier versions are encrypted on first start.

## Translations

English is the source language. To add a language:

1. Copy `web/src/i18n/es.ts` (UI) and `internal/i18n/es.go` (API errors and CLI). Keys are the English strings; `ctx|text` keys are gender/number variants used by `tc()`.
2. Register the new catalog in `web/src/i18n/index.ts` and `internal/i18n/i18n.go`.

## Development

```sh
make help           # show all available make targets
make run            # build and run ducktainer serve locally (http://127.0.0.1:9900)
make ui             # build and run ducktainer ui (opens browser)
make build          # web UI + static binary (no PAM) in bin/
make build-pam      # with PAM (needs gcc and pam-devel / libpam0g-dev)
make test           # run all tests (Go unit tests + web typecheck)
make dev            # UI with hot reload against a running `ducktainer serve` on :9900
make packages       # rpm + deb (amd64, arm64) built in a container; only Docker needed
make dist           # static binaries for linux/darwin/windows
```

Layout:

- `main.go`: CLI.
- `internal/server`: HTTP/WebSocket API.
- `internal/docker`: Engine API client (unix, ssh, tcp+tls).
- `internal/auth`: PAM, roles and the root helper.
- `internal/setup`: Docker install detection.
- `internal/compose`, `internal/templates`, `internal/proxy`, `internal/access`, `internal/i18n`.
- `packaging/`: systemd units, PAM files, nfpm.
- `web/`: React + Vite.
