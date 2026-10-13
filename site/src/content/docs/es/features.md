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

Unas 26 apps incluidas más cualquier lista de plantillas de Portainer v2/v3. Las listas oficial y de la comunidad, unas 800 apps, vienen preconfiguradas. Cada app se despliega como un stack editable.

## Auditoría

Cada inicio de sesión, cada cambio y cada sesión de terminal queda registrado, en **Configuración → Auditoría** y en `journalctl -u ducktainer`.
