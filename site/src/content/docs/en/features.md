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

## Audit log

Every sign-in, every change and every terminal session is recorded, in **Settings → Audit** and in `journalctl -u ducktainer`.
