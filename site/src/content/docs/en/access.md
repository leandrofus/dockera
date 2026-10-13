---
title: Users and access
description: Roles from system groups, opening ducktainer to the network, TLS and signing in.
order: 3
---

## Give people access

Accounts are ordinary system users, local or from LDAP/SSSD/FreeIPA. Their **role** depends on their groups:

| Role | Groups (default) | Can |
|---|---|---|
| **Administrator** | `ducktainer-admins`, `docker` | everything: containers, stacks, images, terminals, settings, users |
| **Read-only** | `ducktainer-viewers` | see containers, stacks, images, networks, volumes and hosts; cannot change anything, open terminals, browse or download files, or see other settings. Passwords in connection strings are hidden |

```sh
sudo useradd -m ana && sudo passwd ana       # only if the account doesn't exist yet
sudo usermod -aG ducktainer-admins ana       # administrator (or ducktainer-viewers: read-only)
sudo ducktainer auth test ana                # checks the password and shows the resulting role
```

- Group membership is re-checked every 5 minutes, so removing someone from the group revokes their access quickly.
- `root` cannot sign in unless you enable `allow_root`.
- The UI shows all of this under **Settings → Access → People**: the role → group mapping, the current members of each group, and a guide that writes these commands for you.

## Open it to the network

By default ducktainer only listens on the local machine. Allow the addresses that may connect and restart. They can be IPs, CIDR ranges or host names; host names are re-resolved every minute.

```sh
sudo ducktainer allow add 192.168.1.0/24 office.example.com
sudo systemctl restart ducktainer       # now it listens on 0.0.0.0:9900, with TLS
```

- Anyone else is disconnected before TLS. The local machine is always allowed.
- **Settings → Access → Network** shows the listen address, your own IP and the rule that lets it in. You can edit the allowlist there and changes apply immediately. There is also a "would this address be allowed?" checker.
- The TLS certificate is self-signed. Its SHA-256 fingerprint is printed at startup so clients can pin it. To use your own certificate, pass `--cert/--key` in `/etc/ducktainer/ducktainer.env`.

## Sign in

Open `https://<server>:9900` and sign in with your system user and password.

The **emergency token** signs in as administrator and authorizes the API (`Authorization: Bearer`). Only its hash is stored, so it is shown once: `sudo ducktainer token --rotate` creates and prints a new one. To allow only system users, turn it off with `sudo ducktainer auth set --token-login off`.
