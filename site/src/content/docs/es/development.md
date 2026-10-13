---
title: Desarrollo
description: Compilar ducktainer desde el código, la estructura del proyecto y añadir una traducción.
order: 8
---

## Compilar

```sh
make help           # ver todos los comandos disponibles
make run            # compilar y ejecutar ducktainer serve localmente (http://127.0.0.1:9900)
make ui             # compilar y ejecutar ducktainer ui (abre el navegador)
make build          # UI web + binario estático (sin PAM) en bin/
make build-pam      # con PAM (necesita gcc y pam-devel / libpam0g-dev)
make test           # todas las pruebas (tests de Go + chequeo de tipos web)
make dev            # UI con hot reload contra un `ducktainer serve` en :9900
make packages       # rpm + deb (amd64, arm64) construidos en un contenedor; solo hace falta Docker
make dist           # binarios estáticos para linux/darwin/windows
```

## Estructura

- `main.go`: la CLI.
- `internal/server`: API HTTP/WebSocket.
- `internal/docker`: cliente de la Engine API (unix, ssh, tcp+tls, npipe).
- `internal/auth`: PAM, roles y el ayudante root.
- `internal/setup`: detección de la instalación de Docker.
- `internal/engine`: instalación de Docker en Windows.
- `internal/compose`, `internal/templates`, `internal/proxy`, `internal/access`, `internal/i18n`.
- `packaging/`: units de systemd, archivos PAM, nfpm.
- `web/`: React + Vite.
- `site/`: este sitio web (Astro).

## Traducciones

El inglés es el idioma de origen. Para añadir un idioma:

1. Copia `web/src/i18n/es.ts` (UI) e `internal/i18n/es.go` (errores de la API y CLI). Las claves son los textos en inglés; las claves `ctx|texto` son variantes de género y número que usa `tc()`.
2. Registra el nuevo catálogo en `web/src/i18n/index.ts` y en `internal/i18n/i18n.go`.

## Versiones

La versión está en `web/package.json`. Para publicar una, súbela en tu pull request: cuando el merge llega a `main`, el workflow Tag crea el tag `v<versión>` y lanza el release, que compila los paquetes y binarios y reconstruye este sitio. Los merges que no cambian la versión no publican nada.
