---
title: Features
description: Containers, compose stacks, images, networks, volumes, the marketplace and the audit log.
order: 5
---

## Containers

A table grouped by stack, or a kanban board by state: drag a card to "Stopped" to stop it. There are saved views, bulk actions and a quick view. Each container has Overview (live CPU, memory and network, plus activity), Logs, Terminal, Files, Performance, Processes, Configuration and Inspect.

### Editing containers

Docker can't modify a container in place. ducktainer creates a new one with the edited settings, swaps the names and removes the old one. Volumes are kept, and the original is restored if the new one fails to start.

Containers can also be duplicated, updated (pull and recreate) and committed to an image.

## Compose stacks

- A **visual editor** and a YAML editor. The YAML stays the source of truth: comments, formatting and variables are preserved.
- Multi-file projects (base plus overrides), with a merged view and "merge into one file".
- Validation before saving (`docker compose config`), deploy and update, and combined logs per service.
- Stacks created outside ducktainer are detected and edited where they are.

## Images

Pull and push with per-layer progress, tag, build from a Dockerfile, layer history and cleanup.

## Networks

A list with the connected containers and their IPs, a connection map, and create, connect and disconnect.

## Volumes

Who uses each volume, orphans, sizes, and browsing their contents.

## Marketplace

About 26 built-in apps plus any Portainer v2/v3 template list. The official and community lists, around 800 apps, are preconfigured. Each app deploys as a stack you can edit.

## Tunnels

The **Tunnels** tab of Jump has two lists: **Private**, ports forwarded through this ducktainer server, and **Public**, published through Cloudflare (below). Tick "All hosts" to see the tunnels of every host.

A private tunnel opens a port of the server, from the `--tunnel-ports` range, that leads to the container. It also works for internal ports and for hosts behind an agent or NAT. Right-click a port and choose **Open a tunnel…**, then pick how long it lasts:

- **Temporary**: closes after 30 minutes without connections. This is what "Open via tunnel" uses.
- **For some hours**: closes at a fixed time.
- **Persistent**: stays until you close it.

Tunnels for some hours and persistent ones come back after a restart, on the same port when it is free. A tunnel only lets in the address that opened it, or, if you choose so, every address allowed in Settings → Access → Network. Tunnels follow the container by name, so recreating it keeps them working. From the list you can change a tunnel's duration and access, or close it.

## Exposing to the internet (Cloudflare)

Right-click a web port in **Jump** and choose **Expose to the internet…**. ducktainer runs `cloudflared` itself (the installed one, or a pinned, checksum-verified download), so nothing is added to your Docker hosts. It also works for internal ports and for hosts behind an agent.

- **Quick tunnel**, no account: a random `trycloudflare.com` address that changes when ducktainer restarts.
- **Your domain**: connect a Cloudflare account in **Settings → Cloudflare** with an API token (Cloudflare Tunnel: Edit, DNS: Edit, Zone: Read). ducktainer creates a tunnel for the server, and each exposure gets a name such as `app.example.com` with its own DNS record.

Who can open it:

- **Public**: anyone with the address.
- **Sign in with a system account**: everyone who can use ducktainer, or only some users or groups.
- **Secret link**: whoever opens it gets in, without an account. A new link revokes the old one.
- **Only from these addresses**: IPs, CIDR ranges or hostnames. This rule is combined with the ones above, and both must pass.

Exposures can close themselves after a while. They are listed under Public in the **Tunnels** tab of Jump and in Settings → Cloudflare, and sign-ins are written to the audit log.

## Start menu

On a machine with a desktop (Linux or Windows), **Settings → Access → Start menu** adds the web apps of its own containers to the menu. On Linux they go in a "Containers" submenu; on Windows, in Start › Programs › Containers. Each entry opens `http://localhost:<port>` in the default browser. Pick the containers one by one (also from the Jump menu), or let ducktainer add every web container. Entries follow the containers when they are recreated.

## Audit log

Every sign-in, every change and every terminal session is recorded, in **Settings → Audit** and in `journalctl -u ducktainer`.
