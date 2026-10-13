---
title: Archivos y secretos
description: Dónde guarda ducktainer su configuración y cómo se cifran los secretos.
order: 7
---

## Archivos

| Ruta | Contenido |
|---|---|
| `/etc/ducktainer/ducktainer.env` | `DUCKTAINER_OPTS` para `ducktainer serve` |
| `/etc/pam.d/ducktainer` | pila PAM para entrar (`password-auth` en RHEL, `common-auth` en Debian) |
| `/var/lib/ducktainer/` | configuración, stacks, auditoría, certificado TLS y las claves SSH del servicio |
| `/etc/systemd/system/ducktainer.service.d/10-docker-access.conf` | lo escribe `ducktainer setup` |
| `/etc/ducktainer/secret.key` | clave que cifra los secretos de la configuración (solo root; se pasa al servicio con `LoadCredential` de systemd) |

## Secretos

Los secretos de `config.json` (secreto de sesión, tokens de hosts, contraseñas de registries) se cifran con AES-256-GCM. El token de emergencia se guarda solo como hash SHA-256.

La clave se guarda aparte de los datos, así que una copia o backup de `/var/lib/ducktainer` no revela nada sin ella. **Haz copia de `/etc/ducktainer/secret.key` por separado**: sin ella no se puede leer la configuración.

Si ejecutas el binario sin el paquete, la clave es `secret.key` en el directorio de datos, salvo que `DUCKTAINER_KEY_FILE` apunte a otro sitio. Las configuraciones en texto plano de versiones anteriores se cifran al primer arranque.
