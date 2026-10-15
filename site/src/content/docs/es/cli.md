---
title: Línea de comandos
description: Todos los comandos de ducktainer y sus opciones principales.
order: 6
---

| Comando | Para qué sirve |
|---|---|
| `ducktainer serve` | UI web y API (`--listen`, `--allow`, `--tls auto\|on\|off`, `--cert/--key`, `--expose :2376`, `--agent-listen :9901`) |
| `ducktainer ui` | Lo mismo que `serve`, y abre el navegador |
| `ducktainer agent` | Agente saliente (`--server`, `--name`, `--fingerprint`, `--socket`, `--install-service`; token en `DUCKTAINER_AGENT_TOKEN`) |
| `ducktainer expose` | Publica solo la API de Docker, protegida con lista de direcciones, token y TLS (`--read-only`, `--no-exec`) |
| `ducktainer engine install [--mode wsl\|native] \| status \| relay` | Windows: instala Docker sin Docker Desktop |
| `ducktainer setup [--dry-run]` | Adapta el servicio a la instalación de Docker |
| `ducktainer auth show \| set … \| test <usuario>` | Entrada con usuarios del sistema y asignación de roles |
| `ducktainer allow ls \| add \| rm` | Lista de direcciones permitidas |
| `ducktainer token --rotate` | Nuevo token de emergencia/API (solo se guarda su hash) |
| `ducktainer endpoint ls \| add \| rm` | Hosts Docker (`unix://`, `ssh://usuario@host`, `tcp://host:2376`, `agent://nombre`) |
| `ducktainer registry ls \| add \| rm` | Credenciales de registries privados (`--hosts` limita qué hosts las reciben) |
| `ducktainer ps \| logs \| sh` | Contenedores por stack, logs de un contenedor o de un stack entero, shell interactiva (elige bash/ash/sh) |
| `ducktainer docker-env <host>` | `eval "$(ducktainer docker-env prod)"` para usar el CLI de docker contra un host |
| `ducktainer version` | Muestra la versión |

`ducktainer help` muestra dónde se guardan la configuración y los datos; se cambia con `DUCKTAINER_HOME`.
