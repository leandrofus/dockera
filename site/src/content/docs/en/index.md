---
title: Introduction
description: What ducktainer is, how it reaches your Docker hosts and how people sign in.
order: 1
---

ducktainer manages Docker from a single binary. You get a web UI with right-click everywhere, saved views, a kanban board and a ⌘K palette. It also has compose stacks with a visual editor, an app marketplace, live logs and web terminals.

People sign in with their **system users** (PAM). Network access is limited to the addresses you allow.

## How it works

- **No agents required.** Remote hosts are reached over **SSH** (`docker system dial-stdio`, like `DOCKER_HOST=ssh://`) or through `ducktainer expose`. They only need Docker. For hosts behind NAT there is an optional outbound agent.
- **Least privilege.** The service runs as an unprivileged user. `ducktainer setup` adapts it to however Docker is installed: socket group, rootless or root-only. Only password checks run as root, in a helper started on demand.
- **Access control.** An IP/CIDR/hostname allowlist drops connections before TLS. Roles come from POSIX groups. TLS is automatic, with certificate pinning, and every action goes to an audit log.
- **Languages.** English and Spanish. The UI follows the browser and can be switched from the user menu; the CLI follows `LANG`.

## Where to start

1. [Install](install/) the package and check how it reaches Docker.
2. [Give people access](access/) and open it to your network.
3. [Add more hosts](hosts/) over SSH, the agent or `ducktainer expose`.

The [command line](cli/) and [files](files/) pages are the reference for everything else.
