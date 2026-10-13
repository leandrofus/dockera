---
title: Introducción
description: Qué es ducktainer, cómo llega a tus hosts Docker y cómo entra la gente.
order: 1
---

ducktainer administra Docker desde un único binario. Incluye una UI web con clic derecho en todo, vistas guardadas, un tablero kanban y una paleta ⌘K. También tiene stacks compose con editor visual, un marketplace de apps, logs en vivo y terminales web.

Se entra con los **usuarios del sistema** (PAM), y el acceso por red se limita a las direcciones que autorices.

## Cómo funciona

- **Sin agentes obligatorios.** Los hosts remotos se alcanzan por **SSH** (`docker system dial-stdio`, como `DOCKER_HOST=ssh://`) o con `ducktainer expose`. Solo necesitan Docker. Para hosts detrás de NAT hay un agente saliente opcional.
- **Mínimo privilegio.** El servicio corre como usuario sin privilegios. `ducktainer setup` lo adapta a cómo esté instalado Docker: grupo del socket, rootless o solo root. Solo la comprobación de contraseñas corre como root, en un ayudante que arranca a demanda.
- **Control de acceso.** Una lista de IPs, rangos CIDR y nombres de host corta las conexiones antes del TLS. Los roles salen de los grupos POSIX. El TLS es automático, con certificado fijado, y cada acción queda en la auditoría.
- **Idiomas.** Inglés y español. La UI sigue el idioma del navegador y se cambia desde el menú de usuario; la CLI usa `LANG` (por ejemplo `LANG=es_AR.UTF-8 ducktainer help`).

## Por dónde empezar

1. [Instala](install/) el paquete y comprueba cómo llega a Docker.
2. [Da acceso a las personas](access/) y ábrelo a tu red.
3. [Añade más hosts](hosts/) por SSH, con el agente o con `ducktainer expose`.

Las páginas de [línea de comandos](cli/) y [archivos](files/) son la referencia para todo lo demás.
