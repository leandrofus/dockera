---
title: Install
description: Packages for RHEL, Fedora, Debian and Ubuntu, the single binary, Docker on Windows, and how ducktainer reaches Docker.
order: 2
---

## Packages

The packages are the recommended way. They work on RHEL/Alma/Rocky 9+, Fedora, Debian 12+ and Ubuntu 22.04+, on amd64 and arm64. Docker must already be installed, because the package depends on it.

Download the package for your system from the [latest release](https://github.com/leandrofus/ducktainer/releases/latest), then:

```sh
sudo dnf install ./ducktainer-0.1.0-1.x86_64.rpm     # RHEL / Fedora
sudo apt install ./ducktainer_0.1.0-1_amd64.deb      # Debian / Ubuntu
```

The package:

- creates the `ducktainer` user and the `ducktainer-admins` / `ducktainer-viewers` groups;
- runs `ducktainer setup` to detect how Docker is installed;
- enables the `ducktainer` service, listening on `http://127.0.0.1:9900`.

## Single binary

The binary runs on any Linux, macOS or Windows. Without the package there is no PAM, so you sign in with the token it prints at startup.

```sh
./ducktainer serve            # http://127.0.0.1:9900, prints the access token
./ducktainer ui               # same, and opens the browser
```

## Windows without Docker Desktop

`ducktainer engine` installs the open-source Docker Engine for you. Run it from an administrator terminal:

```powershell
.\ducktainer.exe engine install                 # Linux containers: Docker in a WSL 2 distribution of its own
.\ducktainer.exe engine install --mode native   # Windows containers: dockerd.exe as a Windows service
.\ducktainer.exe engine status
.\ducktainer.exe ui
```

- **`wsl`** (the default) creates the `ducktainer` WSL 2 distribution from Ubuntu's image, checked against Ubuntu's SHA256SUMS. It runs `docker.io` there with systemd and adds the `wsl` host (`wsl://ducktainer`). A logon task keeps the distribution running, so containers don't stop when WSL would shut it down. WSL distributions belong to a user: run ducktainer as that user.
- **`native`** enables the Containers feature (and Hyper-V on Windows 10/11), installs `dockerd.exe`, `docker.exe` and the compose plugin in `C:\Program Files\Docker`, and registers the `docker` service. The `local` host then points at `npipe:////./pipe/docker_engine`. Windows Home has no Containers feature: use `wsl`.
- If Windows needs a restart to enable WSL or a feature, restart and run the same command again.

## Check how ducktainer reaches Docker

```sh
sudo ducktainer setup --dry-run
```

```
socket:  /var/run/docker.sock  (root:docker 0660, standard path)
mode:    group
  · access through group docker (socket root:docker 0660)
```

| Detected installation | The service runs as |
|---|---|
| Socket accessible by a group (`root:docker 0660`, any group name) | `ducktainer` + that group |
| Rootless Docker (`/run/user/1000/docker.sock`) | the socket owner |
| Root-only socket, Podman | root (with a warning) |
| Installed but not started yet | what the `docker.socket` unit says |

Run `sudo ducktainer setup` again whenever the Docker installation changes.
