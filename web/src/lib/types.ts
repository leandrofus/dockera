export interface Port {
  IP?: string
  PrivatePort: number
  PublicPort?: number
  Type: string
}

export interface Mount {
  Type: string
  Name?: string
  Source: string
  Destination: string
  RW: boolean
}

export interface Container {
  Id: string
  Names: string[]
  Image: string
  ImageID: string
  Command: string
  Created: number
  State: 'running' | 'exited' | 'paused' | 'restarting' | 'created' | 'dead' | 'removing'
  Status: string
  Ports: Port[]
  Labels: Record<string, string>
  Mounts: Mount[]
  NetworkSettings: { Networks: Record<string, { IPAddress: string; NetworkID: string; MacAddress: string }> }
  SizeRw?: number
  SizeRootFs?: number
}

export interface Attached {
  id: string
  name: string
  state: string
  ipv4?: string
  mac?: string
  dest?: string
}

export interface Network {
  Id: string
  Name: string
  Driver: string
  Scope: string
  Internal: boolean
  Attachable: boolean
  EnableIPv6: boolean
  Created: string
  IPAM: { Config: { Subnet?: string; Gateway?: string; IPRange?: string }[] | null }
  Labels: Record<string, string> | null
  Options: Record<string, string> | null
  Attached: Attached[]
}

export interface Volume {
  Name: string
  Driver: string
  Mountpoint: string
  CreatedAt: string
  Labels: Record<string, string> | null
  Scope: string
  Options: Record<string, string> | null
  UsedBy: Attached[]
  Size?: number
}

export interface Image {
  Id: string
  RepoTags: string[] | null
  RepoDigests: string[] | null
  Created: number
  Size: number
  Containers: number
  Labels: Record<string, string> | null
  UsedBy: string[] | null
}

export interface StackContainer {
  id: string
  name: string
  service: string
  state: string
  status: string
  image: string
}

export interface Stack {
  name: string
  managed: boolean
  working_dir?: string
  config_files?: string[]
  services: string[]
  containers: StackContainer[]
  running: number
  total: number
  status: 'running' | 'partial' | 'stopped' | 'empty'
  editable: boolean
  reason?: string
  env_file?: string
  // kept in its own directory instead of the dockera store
  location?: StackLocation
  // files live on the remote node (compose runs there)
  node?: boolean
}

export interface StackLocation {
  dir: string
  file?: string
  env_file?: string
}

export interface StackFile {
  path: string
  name: string
  content: string
}

export interface StackFiles {
  files: StackFile[]
  env: string
  env_path: string
  dir: string
  managed: boolean
  node?: boolean
}

export interface Endpoint {
  name: string
  url: string
  token?: string
  tls?: boolean
  fingerprint?: string
  ssh_key?: string
  docker_socket?: string
  compose_image?: string
  online: boolean
  error?: string
  version?: string
  os?: string
  running: number
  total: number
}

export interface Registry {
  name: string
  server: string
  username: string
  password: string
}

export interface Spec {
  name: string
  image: string
  cmd: string[] | null
  entrypoint: string[] | null
  env: string[] | null
  ports: { host_ip: string; host_port: string; container: string }[] | null
  mounts: { type: string; source: string; target: string; readonly: boolean }[] | null
  restart: string
  labels: Record<string, string> | null
  networks: { name: string; aliases: string[] | null; ipv4: string }[] | null
  network_mode: string
  hostname: string
  user: string
  working_dir: string
  privileged: boolean
  tty: boolean
  open_stdin: boolean
  memory: number
  nano_cpus: number
  cap_add: string[] | null
  extra_hosts: string[] | null
}

export interface TemplateEnv {
  name: string
  label?: string
  default?: string
  description?: string
  preset?: boolean
  select?: { text: string; value: string; default?: boolean }[]
}

export interface Template {
  id: string
  source: string
  type: 1 | 2 | 3
  name?: string
  title: string
  description: string
  note?: string
  logo?: string
  categories?: string[]
  image?: string
  ports?: string[]
  volumes?: { container: string; bind?: string; readonly?: boolean }[]
  env?: TemplateEnv[]
  command?: string
  restart_policy?: string
  labels?: { name: string; value: string }[]
  privileged?: boolean
  interactive?: boolean
  hostname?: string
  network?: string
  repository?: { url: string; stackfile: string }
  compose?: string
}

export interface DockerEvent {
  Type: string
  Action: string
  Actor: { ID: string; Attributes: Record<string, string> }
  time: number
  timeNano: number
  scope?: string
}

export interface Stats {
  read: string
  cpu: number
  ncpu: number
  mem: number
  mem_limit: number
  net_rx: number
  net_tx: number
  blk_read: number
  blk_write: number
  pids: number
}

export interface LogLine {
  t?: string
  s: 'out' | 'err'
  m: string
  c?: string
}
