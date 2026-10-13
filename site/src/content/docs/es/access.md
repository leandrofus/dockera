---
title: Usuarios y acceso
description: Roles a partir de los grupos del sistema, abrir ducktainer a la red, TLS y cómo entrar.
order: 3
---

## Dar acceso a personas

Las cuentas son usuarios normales del sistema, locales o de LDAP/SSSD/FreeIPA. El **rol** sale de sus grupos:

| Rol | Grupos (por defecto) | Puede |
|---|---|---|
| **Administrador** | `ducktainer-admins`, `docker` | todo: contenedores, stacks, imágenes, terminales, configuración y usuarios |
| **Solo lectura** | `ducktainer-viewers` | ver todo salvo la configuración, sin modificar nada, abrir terminales ni descargar archivos |

```sh
sudo useradd -m ana && sudo passwd ana       # solo si la cuenta no existe
sudo usermod -aG ducktainer-admins ana       # administradora (o ducktainer-viewers: solo lectura)
sudo ducktainer auth test ana                # comprueba la contraseña y muestra el rol
```

- La pertenencia a los grupos se revisa cada 5 minutos: si quitas a alguien de un grupo, pierde el acceso enseguida.
- `root` no puede entrar salvo que actives `allow_root`.
- La UI lo muestra en **Configuración → Acceso → Quién puede entrar**: los grupos de cada rol, sus miembros actuales y una guía que te escribe estos comandos.

## Abrirlo a la red

Por defecto ducktainer solo escucha en la propia máquina. Autoriza las direcciones que pueden conectarse y reinicia. Pueden ser IPs, rangos CIDR o nombres de host; los nombres se vuelven a resolver cada minuto.

```sh
sudo ducktainer allow add 192.168.1.0/24 oficina.ejemplo.com
sudo systemctl restart ducktainer       # ahora escucha en 0.0.0.0:9900, con TLS
```

- Cualquier otro origen se desconecta antes del TLS. La propia máquina siempre está permitida.
- En **Configuración → Acceso → Desde dónde** ves la dirección de escucha, tu IP y la regla que te deja pasar. Puedes editar la lista ahí y los cambios se aplican al instante. También puedes comprobar si una dirección estaría permitida.
- El certificado TLS es autofirmado. Su huella SHA-256 se imprime al arrancar para que los clientes puedan fijarla. Para usar tu propio certificado, pasa `--cert/--key` en `/etc/ducktainer/ducktainer.env`.

## Entrar

Abre `https://<servidor>:9900` y entra con tu usuario y contraseña del sistema.

El **token de emergencia** entra como administrador y autoriza la API (`Authorization: Bearer`). Solo se guarda su hash, así que se muestra una vez: `sudo ducktainer token --rotate` crea e imprime uno nuevo. Para permitir solo usuarios del sistema, desactívalo con `sudo ducktainer auth set --token-login off`.
