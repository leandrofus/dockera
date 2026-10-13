# dockera

**Administración de Docker sin agente, en un único binario.** Incluye:

- UI web tipo CRM: clic derecho en todo, vistas guardadas, kanban y paleta ⌘K;
- stacks compose con editor visual;
- marketplace de apps;
- logs en vivo y terminales web.

Se entra con los **usuarios del sistema** (PAM), y el acceso por red se limita a los hosts que autorices.

[English](README.md)

## Puesta en marcha

### 1. Instalar

**Paquetes (recomendado).** Sirven para RHEL/Alma/Rocky 9+, Fedora, Debian 12+ y Ubuntu 22.04+ (amd64 y arm64). Docker tiene que estar instalado: el paquete depende de él.

```sh
sudo dnf install ./dockera-0.1.0-1.x86_64.rpm     # RHEL / Fedora
sudo apt install ./dockera_0.1.0-1_amd64.deb      # Debian / Ubuntu
```

El paquete:

- crea el usuario `dockera` y los grupos `dockera-admins` y `dockera-viewers`;
- detecta cómo está instalado Docker (`dockera setup`);
- deja el servicio `dockera` escuchando en `http://127.0.0.1:9900`.

**Binario suelto.** Funciona en Linux, macOS y Windows, pero solo con token (sin PAM):

```sh
./dockera serve       # http://127.0.0.1:9900; muestra el token
```

### 2. Comprobar cómo llega a Docker

```sh
sudo dockera setup --dry-run
```

| Instalación detectada | El servicio corre como |
|---|---|
| Socket accesible por un grupo (`root:docker 0660`, con cualquier nombre de grupo) | `dockera` + ese grupo |
| Docker rootless (`/run/user/1000/docker.sock`) | el dueño del socket |
| Socket solo de root, o Podman | root (con aviso) |
| Docker instalado pero sin arrancar | lo que indique la unit `docker.socket` |

Si cambias la instalación de Docker, vuelve a ejecutar `sudo dockera setup`.

### 3. Dar acceso a personas

Las cuentas son usuarios normales del sistema (locales o de LDAP/SSSD/FreeIPA). El **rol** sale de sus grupos:

| Rol | Grupos (por defecto) | Puede |
|---|---|---|
| **Administrador** | `dockera-admins`, `docker` | todo: contenedores, stacks, imágenes, terminales, configuración y usuarios |
| **Solo lectura** | `dockera-viewers` | ver todo salvo la configuración, sin modificar nada, abrir terminales ni descargar archivos |

```sh
sudo useradd -m ana && sudo passwd ana       # solo si la cuenta no existe
sudo usermod -aG dockera-admins ana          # administradora (o dockera-viewers: solo lectura)
sudo dockera auth test ana                   # comprueba la contraseña y muestra el rol
```

- La pertenencia a los grupos se revisa cada 5 minutos: si quitas a alguien de un grupo, pierde el acceso enseguida.
- La UI lo muestra en **Configuración → Acceso → Quién puede entrar**: los grupos de cada rol, sus miembros actuales y una guía que te escribe estos comandos.

### 4. Abrirlo a la red

Por defecto dockera solo escucha en la propia máquina. Autoriza las direcciones que pueden conectarse (IPs, rangos CIDR o nombres de host) y reinicia:

```sh
sudo dockera allow add 192.168.1.0/24 oficina.ejemplo.com
sudo systemctl restart dockera       # ahora escucha en 0.0.0.0:9900, con TLS
```

- Cualquier otro origen se desconecta antes del TLS.
- En **Configuración → Acceso → Desde dónde** ves la dirección de escucha, tu IP y la regla que te deja pasar. Puedes editar la lista (se aplica al instante) y comprobar si una dirección estaría permitida.

### 5. Entrar

Abre `https://<servidor>:9900` y entra con tu usuario y contraseña del sistema.

El **token de emergencia** entra como administrador y autoriza la API. Solo se guarda su hash, así que se muestra una vez: `sudo dockera token --rotate` crea e imprime uno nuevo. Se desactiva con `sudo dockera auth set --token-login off`.

### 6. Más hosts Docker (opcional)

**Por Agente (conexión saliente / Edge):**
Ideal para nodos detrás de NAT, cortafuegos o con IP dinámica. El nodo descarga el agente standalone desde el propio servidor de Dockera y se conecta hacia él por un túnel saliente:
```sh
# Descarga e instala en el nodo remoto (como servicio systemd o proceso en segundo plano):
curl -kfsSL "https://<servidor-dockera>:9900/api/agent/install.sh?token=<token>&name=worker-1" | sh

# O ejecutándolo directamente de forma manual:
curl -kfsSL "https://<servidor-dockera>:9900/api/agent/binary?token=<token>" -o dockera-agent && chmod +x dockera-agent
sudo ./dockera-agent agent --server "https://<servidor-dockera>:9900" --token "<token>" --name "worker-1"
```

**Por SSH (sin agente):**
```sh
dockera endpoint add prod ssh://deploy@prod.ejemplo.com     # por SSH, nada que instalar en el remoto
```

**Por TCP (proxy dockera expose):**
```sh
dockera expose --allow <ip-del-servidor-dockera>             # o el proxy, ejecutado en el remoto
```

## Idiomas

La UI está en inglés y en español: sigue el idioma del navegador y se cambia desde el menú de usuario. La CLI usa `LANG` (por ejemplo `LANG=es_AR.UTF-8 dockera help`).

Para añadir un idioma:

1. Copia `web/src/i18n/es.ts` y `internal/i18n/es.go`; las claves son los textos en inglés.
2. Registra el nuevo catálogo en `web/src/i18n/index.ts` y en `internal/i18n/i18n.go`.

## Desarrollo

```sh
make help           # ver todos los comandos disponibles
make run            # compilar y ejecutar dockera serve localmente (http://127.0.0.1:9900)
make ui             # compilar y ejecutar dockera ui (abre el navegador)
make build          # compilar UI web + binario estático (sin PAM) en bin/
make build-pam      # compilar con soporte PAM
make test           # ejecutar todas las pruebas (Go unit tests + chequeo de tipos web)
make dev            # levantar la UI en modo dev con hot reload hacia dockera serve en :9900
```

En [README.md](README.md) está el detalle completo de funcionalidades, comandos, archivos y empaquetado.
