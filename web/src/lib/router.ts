import { useEffect, useState } from 'react'

// Hash routes: #/<endpoint>/<object>[/<id>[/<tab>]]
export interface Route {
  ep: string
  section: string
  id?: string
  tab?: string
  query: URLSearchParams
}

export function parse(hash = location.hash): Route {
  const [path, qs] = hash.replace(/^#\/?/, '').split('?')
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent)
  return {
    ep: parts[0] || '',
    section: parts[1] || 'home',
    id: parts[2],
    tab: parts[3],
    query: new URLSearchParams(qs || ''),
  }
}

export function href(ep: string, section: string, id?: string, tab?: string) {
  return '#/' + [ep, section, id, tab].filter((p) => p !== undefined && p !== '').map((p) => encodeURIComponent(p!)).join('/')
}

export function go(ep: string, section: string, id?: string, tab?: string) {
  location.hash = href(ep, section, id, tab)
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parse())
  useEffect(() => {
    const on = () => setRoute(parse())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return route
}
