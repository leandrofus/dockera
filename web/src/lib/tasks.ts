import { t as tr } from '../i18n'
// Human titles for API requests shown in the tasks tray.
import { peek } from './store'
import { cname, shortId } from './format'
import type { Container } from './types'

const kinds: Record<string, string> = { images: 'images', networks: 'networks', volumes: 'volumes', containers: 'containers', stacks: 'stacks' }

const containerVerbs: Record<string, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  kill: 'Kill',
  pause: 'Pause',
  unpause: 'Resume',
}

const stackVerbs: Record<string, string> = {
  up: 'Deploy',
  redeploy: 'Update',
  pull: 'Pull images',
  down: 'Take down',
  stop: 'Stop',
  start: 'Start',
  restart: 'Restart',
  pause: 'Pause',
  unpause: 'Resume',
}

function containerName(ep: string, id: string) {
  const list = peek<Container[]>(`/e/${encodeURIComponent(ep)}/containers`)
  const c = list?.find((x) => x.Id.startsWith(id) || cname(x) === id)
  return c ? cname(c) : shortId(id)
}

export function taskTitle(method: string, path: string): string {
  const segs = path.split('?')[0].split('/').filter(Boolean).map(decodeURIComponent)
  const del = method === 'DELETE'
  if (segs[0] === 'e' && segs.length >= 3) {
    const ep = segs[1]
    const [kind, id, sub, arg] = segs.slice(2)
    if (kind === 'prune') return tr('Clean up system ({0})', ep)
    if (id === 'prune') return tr('Delete unused {0}', tr(kinds[kind] || kind))
    switch (kind) {
      case 'containers': {
        if (!id) return tr('Create container')
        const n = containerName(ep, id)
        if (del) return tr('Delete {0}', n)
        if (sub === 'action' && arg) return `${tr(containerVerbs[arg] || arg)} ${n}`
        if (sub === 'rename') return tr('Rename {0}', n)
        if (sub === 'commit') return tr('Commit {0}', n)
        if (sub === 'update') return tr('Update {0}', n)
        break
      }
      case 'images':
        if (id === 'tag') return tr('Tag image')
        if (del) return tr('Delete {0}', segs.slice(3).join('/'))
        break
      case 'networks':
        if (!id) return tr('Create network')
        if (del) return tr('Delete network {0}', shortId(id))
        if (sub === 'connect') return tr('Connect to network')
        if (sub === 'disconnect') return tr('Disconnect from network')
        break
      case 'volumes':
        if (!id) return tr('Create volume')
        if (del) return tr('Delete volume {0}', id)
        break
      case 'stacks':
        if (sub === 'action' && arg) return `${tr(stackVerbs[arg] || arg)} ${id}`
        if (sub === 'adopt') return tr('Adopt {0}', id)
        if (sub === 'merge') return tr('Merge files of {0}', id)
        if (sub === 'location') return tr('Location of {0}', id)
        break
      case 'tunnels':
        return tr('Open tunnel')
    }
    return `${method} ${segs.slice(2).join('/')}`
  }
  switch (segs[0]) {
    case 'endpoints':
      return del ? tr('Delete host {0}', segs[1]) : tr('Save host')
    case 'registries':
      return del ? tr('Delete registry {0}', segs[1]) : tr('Save registry')
    case 'settings':
      return segs[1] === 'rotate-token' ? tr('Rotate token') : tr('Save settings')
    case 'tunnels':
      return tr('Close tunnel')
  }
  return `${method} ${path}`
}
