---
title: Development
description: Building ducktainer from source, the code layout and adding a translation.
order: 8
---

## Build

```sh
make help           # show all available make targets
make run            # build and run ducktainer serve locally (http://127.0.0.1:9900)
make ui             # build and run ducktainer ui (opens the browser)
make build          # web UI + static binary (no PAM) in bin/
make build-pam      # with PAM (needs gcc and pam-devel / libpam0g-dev)
make test           # run all tests (Go unit tests + web typecheck)
make dev            # UI with hot reload against a running `ducktainer serve` on :9900
make packages       # rpm + deb (amd64, arm64) built in a container; only Docker needed
make dist           # static binaries for linux/darwin/windows
```

## Layout

- `main.go`: the CLI.
- `internal/server`: HTTP/WebSocket API.
- `internal/docker`: Engine API client (unix, ssh, tcp+tls, npipe).
- `internal/auth`: PAM, roles and the root helper.
- `internal/setup`: Docker install detection.
- `internal/engine`: Docker installation on Windows.
- `internal/compose`, `internal/templates`, `internal/proxy`, `internal/access`, `internal/i18n`.
- `packaging/`: systemd units, PAM files, nfpm.
- `web/`: React + Vite.
- `site/`: this website (Astro).

## Translations

English is the source language. To add a language:

1. Copy `web/src/i18n/es.ts` (UI) and `internal/i18n/es.go` (API errors and CLI). Keys are the English strings; `ctx|text` keys are gender/number variants used by `tc()`.
2. Register the new catalog in `web/src/i18n/index.ts` and `internal/i18n/i18n.go`.

## Releases

The version lives in `web/package.json`. To publish one, bump it in your pull request: when the merge reaches `main`, the Tag workflow creates the `v<version>` tag and starts the release, which builds the packages and binaries and rebuilds this site. Merges that don't change the version publish nothing.
