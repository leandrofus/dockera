import type { Lang } from './i18n'

export const REPO_URL = 'https://github.com/leandrofus/ducktainer'

const base = import.meta.env.BASE_URL.replace(/\/$/, '')

/** The site's home page in a language. */
export function homePath(lang: Lang): string {
  return lang === 'en' ? `${base}/` : `${base}/${lang}/`
}

/** A documentation page; an empty slug is the docs index. */
export function docsPath(lang: Lang, slug = ''): string {
  return `${homePath(lang)}docs/${slug ? `${slug}/` : ''}`
}

/** A file served from the brand directory (favicon, logos). */
export function assetPath(name: string): string {
  return `${base}/${name}`
}
