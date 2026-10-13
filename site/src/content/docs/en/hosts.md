---
title: Docker hosts
description: Manage more Docker hosts over SSH, with the outbound agent or through ducktainer expose.
order: 4
---

ducktainer can manage several Docker hosts. Add them from the command line or from **Settings → Hosts**, and switch between them from the top of the sidebar.

## Over SSH

Nothing to install on the remote host. It needs key-based access, and the remote user must be allowed to use Docker.

```sh
sudo -u ducktainer ssh-keygen -t ed25519      # key in /var/lib/ducktainer/.ssh; add it to the remote's authorized_keys
ducktainer endpoint add prod ssh://deploy@prod.example.com
```

SSH hosts record the host key on the first connection (`StrictHostKeyChecking=accept-new`). A changed key is refused.

## Outbound agent

The agent suits nodes behind NAT, a firewall or a dynamic IP: the node connects out to ducktainer.

1. Add the host, from **Settings → Hosts → Agent** or with the line below. It gets a token of its own.
2. On the node, run the command shown in **Copy agent install command**.

```sh
ducktainer endpoint add worker-1 agent://          # prints the host's token
# on the node:
#   curl -fsSL -k --pinnedpubkey 'sha256//…' -H 'Authorization: Bearer <host token>' \
#     'https://<ducktainer-server>:9900/api/agent/install.sh?server=…' | sudo sh
```

- Each agent host has its own token, valid only for that host. The admin token is not accepted from agents.
- The install command sends the token in a header, pins this server's certificate (no `--insecure`) and leaves it on the node in `/etc/ducktainer-agent/agent.env`, readable only by root.
- Hosts registered by earlier versions with the admin token are refused until you give them a token in Settings → Hosts and reinstall the agent.

## ducktainer expose

`ducktainer expose` publishes only the Docker API of a host, behind an allowlist, a token and TLS. Run it on the remote host; it prints the exact `ducktainer endpoint add …` line to run on the server.

```sh
ducktainer expose --allow <ducktainer-server-ip>
```

Add `--read-only` or `--no-exec` to limit what the server can do. `tcp://` hosts must use TLS unless they are on the same machine. Without `--fingerprint`, the certificate presented when the host is added is pinned.

## Registry credentials

Credentials for private registries can be limited to some hosts, with `--hosts` or in the UI. Compose runs on a node get a copy of only the credentials that host is allowed to see.

```sh
ducktainer registry add ghcr ghcr.io deploy --hosts prod,worker-1    # password on stdin
```

## Trust model

ducktainer controls the Docker API of every host, which is equivalent to root on them. Whoever controls the ducktainer server, or its admin accounts, controls all hosts. Keep that machine and `/etc/ducktainer/secret.key` as protected as the hosts themselves.

To edit stacks on a node, ducktainer starts a helper container there (`ducktainer-files`) that mounts the node's `/`. It exits after an hour idle.
