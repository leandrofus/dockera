---
title: Instalación
description: Paquetes para RHEL, Fedora, Debian y Ubuntu, el binario suelto, Docker en Windows y cómo llega ducktainer a Docker.
order: 2
---

## Paquetes

Los paquetes son la forma recomendada. Sirven para RHEL/Alma/Rocky 9+, Fedora, Debian 12+ y Ubuntu 22.04+, en amd64 y arm64. Docker tiene que estar instalado, porque el paquete depende de él.

Descarga el paquete de tu sistema desde la [última versión](https://github.com/leandrofus/ducktainer/releases/latest) y después:

```sh
sudo dnf install ./ducktainer-0.1.0-1.x86_64.rpm     # RHEL / Fedora
sudo apt install ./ducktainer_0.1.0-1_amd64.deb      # Debian / Ubuntu
```

El paquete:

- crea el usuario `ducktainer` y los grupos `ducktainer-admins` y `ducktainer-viewers`;
- detecta cómo está instalado Docker (`ducktainer setup`);
- deja el servicio `ducktainer` escuchando en `http://127.0.0.1:9900`.

## Binario suelto

El binario funciona en Linux, macOS y Windows. Sin el paquete no hay PAM, así que se entra con el token que imprime al arrancar.

```sh
./ducktainer serve            # http://127.0.0.1:9900; muestra el token
./ducktainer ui               # lo mismo, y abre el navegador
```

## Windows sin Docker Desktop

`ducktainer engine` instala por ti Docker Engine, de código abierto. Ejecútalo desde una terminal de administrador:

```powershell
.\ducktainer.exe engine install                 # contenedores Linux: Docker en una distribución WSL 2 propia
.\ducktainer.exe engine install --mode native   # contenedores Windows: dockerd.exe como servicio de Windows
.\ducktainer.exe engine status
.\ducktainer.exe ui
```

- **`wsl`** (por defecto) crea la distribución WSL 2 `ducktainer` a partir de la imagen de Ubuntu, verificada con los SHA256SUMS de Ubuntu. Ahí corre `docker.io` con systemd y añade el host `wsl` (`wsl://ducktainer`). Una tarea de inicio de sesión mantiene la distribución en marcha, para que los contenedores no se paren cuando WSL la apagaría. Las distribuciones WSL son de un usuario: ejecuta ducktainer como ese usuario.
- **`native`** activa la característica Containers (y Hyper-V en Windows 10/11), instala `dockerd.exe`, `docker.exe` y el plugin compose en `C:\Program Files\Docker` y registra el servicio `docker`. El host `local` pasa a apuntar a `npipe:////./pipe/docker_engine`. Windows Home no tiene Containers: usa `wsl`.
- Si Windows necesita reiniciar para activar WSL o una característica, reinicia y vuelve a ejecutar el mismo comando.

## Comprobar cómo llega a Docker

```sh
sudo ducktainer setup --dry-run
```

```
socket:  /var/run/docker.sock  (root:docker 0660, standard path)
mode:    group
  · access through group docker (socket root:docker 0660)
```

| Instalación detectada | El servicio corre como |
|---|---|
| Socket accesible por un grupo (`root:docker 0660`, con cualquier nombre de grupo) | `ducktainer` + ese grupo |
| Docker rootless (`/run/user/1000/docker.sock`) | el dueño del socket |
| Socket solo de root, o Podman | root (con aviso) |
| Docker instalado pero sin arrancar | lo que indique la unit `docker.socket` |

Si cambias la instalación de Docker, vuelve a ejecutar `sudo ducktainer setup`.
