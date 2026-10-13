# dockler

**Agentless Docker management in a single binary.** A CRM-style web UI (right-click everything, saved views, kanban, ⌘K palette), compose stacks with a visual editor, an app marketplace, live logs and web terminals. Sign-in uses your **system users** (PAM), and network access is limited to the hosts you allow.

[Español](README.es.md)

- **Agentless**: remote hosts are reached over **SSH** (`docker system dial-stdio`, like `DOCKER_HOST=ssh://`) or through `dockler expose`. Nothing but Docker is needed on them.
- **Least privilege**: the service runs as an unprivileged user. `dockler setup` adapts it to however Docker is installed: socket group, rootless or root-only. Only password checks run as root, in a socket-activated helper.
- **Access control**: an IP/CIDR/hostname allowlist (connections are dropped before TLS), roles from POSIX groups, automatic TLS with certificate pinning, and an audit log.
- **Languages**: English and Spanish. The UI follows the browser and can be switched from the user menu; the CLI follows `LANG`.

---

## Getting started

### 1. Install

**Packages (recommended)**: RHEL/Alma/Rocky 9+, Fedora, Debian 12+, Ubuntu 22.04+ (amd64 and arm64). Docker must be installed (the package depends on it).

```sh
sudo dnf install ./dockler-0.1.0-1.x86_64.rpm     # RHEL / Fedora
sudo apt install ./dockler_0.1.0-1_amd64.deb      # Debian / Ubuntu
```

The package:

- creates the `dockler` user and the `dockler-admins` / `dockler-viewers` groups;
- runs `dockler setup` to detect how Docker is installed;
- enables the `dockler` service, listening on `http://127.0.0.1:9900`.

**Single binary** (any Linux, macOS or Windows; token sign-in only, no PAM):

```sh
./dockler serve            # http://127.0.0.1:9900, prints the access token
./dockler ui               # same, and opens the browser
```

### 2. Check how dockler reaches Docker

```sh
sudo dockler setup --dry-run
```

```
socket:  /var/run/docker.sock  (root:docker 0660, standard path)
mode:    group
  · access through group docker (socket root:docker 0660)
```

| Detected installation | The service runs as |
|---|---|
| Socket accessible by a group (`root:docker 0660`, any group name) | `dockler` + that group |
| Rootless Docker (`/run/user/1000/docker.sock`) | the socket owner |
| Root-only socket, Podman | root (with a warning) |
| Installed but not started yet | what the `docker.socket` unit says |

Run `sudo dockler setup` again whenever the Docker installation changes.

### 3. Give people access

Accounts are ordinary system users, local or from LDAP/SSSD/FreeIPA. Their **role** depends on their groups:

| Role | Groups (default) | Can |
|---|---|---|
| **Administrator** | `dockler-admins`, `docker` | everything: containers, stacks, images, terminals, settings, users |
| **Read-only** | `dockler-viewers` | see everything except settings; cannot change anything, open terminals or download files |

```sh
sudo useradd -m ana && sudo passwd ana       # only if the account doesn't exist yet
sudo usermod -aG dockler-admins ana          # administrator  (or dockler-viewers: read-only)
sudo dockler auth test ana                   # checks the password and shows the resulting role
```

- Group membership is re-checked every 5 minutes, so removing someone from the group revokes their access quickly.
- `root` cannot sign in unless you enable `allow_root`.
- The UI shows all of this under **Settings → Access → Who can sign in**: the role → group mapping, the current members of each group, and a guide that writes these commands for you.

### 4. Open it to the network

By default dockler only listens on the local machine. Allow the addresses that may connect (IPs, CIDR ranges or host names; host names are re-resolved every minute) and restart:

```sh
sudo dockler allow add 192.168.1.0/24 office.example.com
sudo systemctl restart dockler       # now it listens on 0.0.0.0:9900, with TLS
```

- Anyone else is disconnected before TLS. The local machine is always allowed.
- **Settings → Access → From where** shows the listen address, your own IP and the rule that lets it in. You can edit the allowlist there; changes apply immediately. There is also a "would this address be allowed?" checker.
- The TLS certificate is self-signed. Its SHA-256 fingerprint is printed at startup so clients can pin it. Use your own certificate with `--cert/--key` in `/etc/dockler/dockler.env`.

### 5. Sign in

Open `https://<server>:9900` and sign in with your system user and password.

The **emergency token** signs in as administrator and authorizes the API (`Authorization: Bearer`). Show it with `sudo dockler token`. To allow only system users, disable it with `sudo dockler auth set --token-login off`.

### 6. Add more Docker hosts (optional)

```sh
# using the outbound agent (ideal for nodes behind NAT/firewall):
# Download and install on the remote node directly from this dockler server:
curl -kfsSL "https://<dockler-server>:9900/api/agent/install.sh?token=<token>&name=worker-1" | sh

# or running the standalone binary manually:
curl -kfsSL "https://<dockler-server>:9900/api/agent/binary?token=<token>" -o dockler-agent && chmod +x dockler-agent
sudo ./dockler-agent agent --server "https://<dockler-server>:9900" --token "<token>" --name "worker-1"

# over SSH: nothing to install on the remote (key-based access; the user must be allowed to use Docker)
sudo -u dockler ssh-keygen -t ed25519          # key in /var/lib/dockler/.ssh, add it to the remote's authorized_keys
dockler endpoint add prod ssh://deploy@prod.example.com

# or through the proxy, on the remote host:
dockler expose --allow <dockler-server-ip>   # prints the exact `dockler endpoint add …` line to run here
```

Hosts can also be managed from **Settings → Hosts**, and you switch between them from the top of the sidebar.

---

## Features

- **Containers**: a table grouped by stack, or a kanban by state (drag a card to "Stopped" to stop it), saved views, bulk actions and quick view. Each record has Overview (live CPU/memory/network plus activity), Logs, Terminal, Files, Performance, Processes, Configuration and Inspect.
- **Editing containers**: Docker can't modify a container in place, so dockler creates a new one with the edited settings, swaps the names and removes the old one. Volumes are kept, and the original is restored if the new one fails to start. Containers can also be duplicated, updated (pull + recreate) and committed to an image.
- **Compose stacks**:
  - a **visual editor** (YAML stays the source of truth: comments, formatting and variables are preserved) and a YAML editor;
  - multi-file projects (base + overrides) with a merged view and "merge into one file";
  - validation before saving (`docker compose config`), deploy and update, combined logs per service;
  - stacks created outside dockler are detected and edited in place.
- **Images**: pull and push with per-layer progress, tag, build from a Dockerfile, layer history, cleanup.
- **Networks**: a registry with connected containers and their IPs, a connection map, create, connect and disconnect.
- **Volumes**: who uses each one, orphans, sizes, and browsing their contents.
- **Marketplace**: about 26 built-in apps plus any Portainer v2/v3 template list (the official and community ~800-app lists are preconfigured).
- **Audit log**: every sign-in and every write or terminal action, in Settings → Audit and in `journalctl -u dockler`.

## Command line

| Command | Purpose |
|---|---|
| `dockler serve` | Web UI + API (`--listen`, `--allow`, `--tls auto\|on\|off`, `--cert/--key`, `--expose :2376`, `--agent-listen :9901`) |
| `dockler agent` | Outbound agent (`--server`, `--token`, `--name`, `--socket`, `--insecure`, `--fingerprint`, `--install-service`) |
| `dockler expose` | Publish only the Docker API, protected (allowlist + token + TLS; `--read-only`, `--no-exec`) |
| `dockler setup [--dry-run]` | Adapt the service to the Docker installation |
| `dockler auth show \| set … \| test <user>` | System-user sign-in and role mapping |
| `dockler allow ls \| add \| rm` | Network allowlist |
| `dockler token [--rotate]` | Emergency/API token |
| `dockler endpoint ls \| add \| rm` | Docker hosts (`unix://`, `ssh://user@host`, `tcp://host:2376`, `agent://name`) |
| `dockler registry ls \| add \| rm` | Private registry credentials |
| `dockler ps \| logs \| sh` | Containers by stack · logs of a container or a whole stack · interactive shell (picks bash/ash/sh) |
| `dockler docker-env <host>` | `eval "$(dockler docker-env prod)"` to use the docker CLI against a host |

## Files

| Path | Contents |
|---|---|
| `/etc/dockler/dockler.env` | `DOCKLER_OPTS` for `dockler serve` |
| `/etc/pam.d/dockler` | PAM stack for sign-in (`password-auth` on RHEL, `common-auth` on Debian) |
| `/var/lib/dockler/` | configuration, stacks, audit log, TLS certificate, the service's SSH keys |
| `/etc/systemd/system/dockler.service.d/10-docker-access.conf` | written by `dockler setup` |

## Translations

English is the source language. To add a language:

1. Copy `web/src/i18n/es.ts` (UI) and `internal/i18n/es.go` (API errors and CLI). Keys are the English strings; `ctx|text` keys are gender/number variants used by `tc()`.
2. Register the new catalog in `web/src/i18n/index.ts` and `internal/i18n/i18n.go`.

## Development

```sh
make help           # show all available make targets
make run            # build and run dockler serve locally (http://127.0.0.1:9900)
make ui             # build and run dockler ui (opens browser)
make build          # web UI + static binary (no PAM) in bin/
make build-pam      # with PAM (needs gcc and pam-devel / libpam0g-dev)
make test           # run all tests (Go unit tests + web typecheck)
make dev            # UI with hot reload against a running `dockler serve` on :9900
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
