---
title: Files and secrets
description: Where ducktainer keeps its configuration, and how secrets are encrypted.
order: 7
---

## Files

| Path | Contents |
|---|---|
| `/etc/ducktainer/ducktainer.env` | `DUCKTAINER_OPTS` for `ducktainer serve` |
| `/etc/pam.d/ducktainer` | PAM stack for sign-in (`password-auth` on RHEL, `common-auth` on Debian) |
| `/var/lib/ducktainer/` | configuration, stacks, audit log, TLS certificate, the service's SSH keys |
| `/etc/systemd/system/ducktainer.service.d/10-docker-access.conf` | written by `ducktainer setup` |
| `/etc/ducktainer/secret.key` | key that encrypts the secrets in the configuration (root only, passed to the service with systemd `LoadCredential`) |

## Secrets

Secrets in `config.json` (session secret, host tokens, registry passwords) are encrypted with AES-256-GCM. The emergency token is stored only as a SHA-256 hash.

The key is kept apart from the data, so a copy or backup of `/var/lib/ducktainer` reveals nothing without it. **Back up `/etc/ducktainer/secret.key` separately**: without it the configuration cannot be read.

For a binary run without the package, the key is `secret.key` in the data directory, unless `DUCKTAINER_KEY_FILE` points elsewhere. Plaintext configurations from earlier versions are encrypted on first start.
