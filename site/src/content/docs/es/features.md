---
title: Funcionalidades
description: Contenedores, stacks compose, imágenes, redes, volúmenes, el marketplace y la auditoría.
order: 5
---

## Contenedores

Una tabla agrupada por stack, o un tablero kanban por estado: arrastra una tarjeta a "Detenidos" para pararlo. Hay vistas guardadas, acciones en lote y vista rápida. Cada contenedor tiene Resumen (CPU, memoria y red en vivo, más actividad), Logs, Terminal, Archivos, Rendimiento, Procesos, Configuración e Inspección.

### Editar contenedores

Docker no puede modificar un contenedor en el sitio. ducktainer crea uno nuevo con la configuración editada, intercambia los nombres y elimina el viejo. Los volúmenes se conservan, y si el nuevo no arranca se restaura el original.

Los contenedores también se pueden duplicar, actualizar (pull y recrear) y guardar como imagen.

## Stacks compose

- Un **editor visual** y un editor YAML. El YAML sigue siendo la fuente de verdad: se respetan comentarios, formato y variables.
- Proyectos de varios archivos (base más overrides), con vista combinada y "unir en un solo archivo".
- Validación antes de guardar (`docker compose config`), despliegue y actualización, y logs combinados por servicio.
- Los stacks creados fuera de ducktainer se detectan y se editan donde están.

## Imágenes

Descarga y sube con progreso por capa, etiqueta, construye desde un Dockerfile, historial de capas y limpieza.

## Redes

Una lista con los contenedores conectados y sus IPs, un mapa de conexiones, y crear, conectar y desconectar.

## Volúmenes

Quién usa cada volumen, huérfanos, tamaños y navegación por su contenido.

## Marketplace

Unas 26 apps incluidas más cualquier lista de plantillas de Portainer v2/v3. La lista oficial de Portainer viene activada; la de la comunidad (unas 800 apps) viene configurada pero desactivada. Cada app se despliega como un stack editable.

## Túneles

La pestaña **Túneles** de Accesos tiene dos listas: **Privados**, puertos reenviados a través de este servidor ducktainer, y **Públicos**, publicados con Cloudflare (abajo). Marca "Todos los hosts" para ver los túneles de todos los hosts.

Un túnel privado abre un puerto del servidor, del rango `--tunnel-ports`, que lleva al contenedor. También funciona con puertos internos y con hosts detrás de un agente o NAT. Haz clic derecho en un puerto, elige **Abrir un túnel…** y decide cuánto dura:

- **Temporal**: se cierra tras 30 minutos sin conexiones. Es lo que usa "Abrir vía túnel".
- **Por horas**: se cierra a una hora fija.
- **Persistente**: queda abierto hasta que lo cierres.

Los túneles por horas y los persistentes vuelven tras un reinicio, en el mismo puerto si está libre. Un túnel solo deja entrar a la dirección que lo abrió o, si lo eliges, a todas las direcciones permitidas en Configuración → Acceso → Red. Los túneles siguen al contenedor por nombre, así que recrearlo no los rompe. Desde la lista puedes cambiar la duración y el acceso de un túnel, o cerrarlo.

## Exponer a internet (Cloudflare)

Haz clic derecho en un puerto web en **Accesos** y elige **Exponer a internet…**. ducktainer ejecuta `cloudflared` él mismo (el instalado, o una descarga fijada y con checksum verificado), así que no agrega nada a tus hosts de Docker. También funciona con puertos internos y con hosts detrás de un agente.

- **Túnel rápido**, sin cuenta: una dirección aleatoria de `trycloudflare.com` que cambia cuando ducktainer se reinicia.
- **Tu dominio**: conecta una cuenta de Cloudflare en **Configuración → Cloudflare** con un token de API (Cloudflare Tunnel: Edit, DNS: Edit, Zone: Read). ducktainer crea un túnel para el servidor, y cada exposición recibe un nombre como `app.ejemplo.com` con su propio registro DNS.

Quién puede abrirlo:

- **Público**: cualquiera que tenga la dirección.
- **Iniciar sesión con una cuenta del sistema**: todos los que pueden usar ducktainer, o solo algunos usuarios o grupos.
- **Link secreto**: quien lo abre entra, sin cuenta. Un link nuevo revoca el anterior.
- **Solo desde estas direcciones**: IPs, rangos CIDR o nombres de host. Esta regla se combina con las anteriores y deben cumplirse ambas.

Las exposiciones pueden cerrarse solas después de un tiempo. Se listan en Públicos, en la pestaña **Túneles** de Accesos y en Configuración → Cloudflare, y los inicios de sesión quedan en la auditoría.

## Menú inicio

En una máquina con escritorio (Linux o Windows), **Configuración → Acceso → Menú inicio** agrega al menú las apps web de sus propios contenedores. En Linux van en un submenú "Containers"; en Windows, en Inicio › Programas › Containers. Cada entrada abre `http://localhost:<puerto>` en el navegador predeterminado. Elige los contenedores uno a uno (también desde el menú de Accesos), o deja que ducktainer agregue todos los contenedores web. Las entradas siguen a los contenedores cuando se recrean, y el menú se actualiza solo: en Linux, `ducktainer setup` instala para todos los usuarios una unidad de usuario de systemd que reconstruye el menú del escritorio (KDE no ve los cambios de otro modo).

## Auditoría

Cada inicio de sesión, cada cambio y cada sesión de terminal queda registrado, en **Configuración → Auditoría** y en `journalctl -u ducktainer`.
