---
title: Hosts Docker
description: Administra más hosts Docker por SSH, con el agente saliente o con ducktainer expose.
order: 4
---

ducktainer puede administrar varios hosts Docker. Añádelos desde la línea de comandos o desde **Configuración → Hosts**, y cambia entre ellos desde la parte superior de la barra lateral.

## Por SSH

Nada que instalar en el host remoto. Necesita acceso por clave, y el usuario remoto tiene que poder usar Docker.

```sh
sudo -u ducktainer ssh-keygen -t ed25519      # clave en /var/lib/ducktainer/.ssh; añádela al authorized_keys del remoto
ducktainer endpoint add prod ssh://deploy@prod.ejemplo.com
```

Los hosts SSH registran la clave del host en la primera conexión (`StrictHostKeyChecking=accept-new`). Una clave cambiada se rechaza.

## Agente saliente

El agente sirve para nodos detrás de NAT, cortafuegos o con IP dinámica: es el nodo el que se conecta a ducktainer.

1. Da de alta el host, en **Configuración → Hosts → Agente** o con la línea de abajo. Recibe un token propio.
2. En el nodo, ejecuta el comando de **Copiar comando de instalación del agente**.

```sh
ducktainer endpoint add worker-1 agent://          # imprime el token del host
# en el nodo:
#   curl -fsSL -k --pinnedpubkey 'sha256//…' -H 'Authorization: Bearer <token del host>' \
#     'https://<servidor-ducktainer>:9900/api/agent/install.sh?server=…' | sudo sh
```

- Cada host con agente tiene su propio token, válido solo para ese host. El token de administrador no se acepta desde agentes.
- El comando de instalación envía el token en una cabecera, fija el certificado de este servidor (sin `--insecure`) y lo deja en el nodo en `/etc/ducktainer-agent/agent.env`, legible solo por root.
- Los hosts registrados por versiones anteriores con el token de administrador se rechazan hasta que les des un token en Configuración → Hosts y reinstales el agente.

## ducktainer expose

`ducktainer expose` publica solo la API de Docker de un host, tras una lista de direcciones, un token y TLS. Ejecútalo en el host remoto; imprime la línea `ducktainer endpoint add …` exacta que tienes que ejecutar en el servidor.

```sh
ducktainer expose --allow <ip-del-servidor-ducktainer>
```

Añade `--read-only` o `--no-exec` para limitar lo que puede hacer el servidor. Los hosts `tcp://` deben usar TLS salvo que estén en la misma máquina. Sin `--fingerprint`, se fija el certificado que presenten al añadirlos.

## Credenciales de registries

Las credenciales de registries privados pueden limitarse a algunos hosts, con `--hosts` o en la UI. Compose ejecutado en un nodo recibe copia solo de las credenciales que ese host puede ver.

```sh
ducktainer registry add ghcr ghcr.io deploy --hosts prod,worker-1    # contraseña por stdin
```

## Modelo de confianza

ducktainer controla la API de Docker de cada host, lo que equivale a root en ellos. Quien controle el servidor ducktainer, o sus cuentas de administrador, controla todos los hosts. Protege esa máquina y `/etc/ducktainer/secret.key` como a los propios hosts.

Para editar stacks en un nodo, ducktainer arranca ahí un contenedor auxiliar (`ducktainer-files`) que monta `/` del nodo. Se cierra tras una hora sin uso.
