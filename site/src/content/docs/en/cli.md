---
title: Command line
description: Every ducktainer command and its main options.
order: 6
---

| Command | Purpose |
|---|---|
| `ducktainer serve` | Web UI and API (`--listen`, `--allow`, `--tls auto\|on\|off`, `--cert/--key`, `--expose :2376`, `--agent-listen :9901`) |
| `ducktainer ui` | Same as `serve`, and opens the browser |
| `ducktainer agent` | Outbound agent (`--server`, `--name`, `--fingerprint`, `--socket`, `--install-service`; token in `DUCKTAINER_AGENT_TOKEN`) |
| `ducktainer expose` | Publish only the Docker API, protected by allowlist, token and TLS (`--read-only`, `--no-exec`) |
| `ducktainer engine install [--mode wsl\|native] \| status \| relay` | Windows: install Docker without Docker Desktop |
| `ducktainer setup [--dry-run]` | Adapt the service to the Docker installation |
| `ducktainer auth show \| set … \| test <user>` | System-user sign-in and role mapping |
| `ducktainer allow ls \| add \| rm` | Network allowlist |
| `ducktainer token --rotate` | New emergency/API token (only its hash is stored) |
| `ducktainer endpoint ls \| add \| rm` | Docker hosts (`unix://`, `ssh://user@host`, `tcp://host:2376`, `agent://name`) |
| `ducktainer registry ls \| add \| rm` | Private registry credentials (`--hosts` limits which hosts get them) |
| `ducktainer ps \| logs \| sh` | Containers by stack, logs of a container or a whole stack, interactive shell (picks bash/ash/sh) |
| `ducktainer docker-env <host>` | `eval "$(ducktainer docker-env prod)"` to use the docker CLI against a host |
| `ducktainer version` | Print the version |

`ducktainer help` shows where configuration and data are kept; change it with `DUCKTAINER_HOME`.
