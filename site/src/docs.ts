import { getCollection, type CollectionEntry } from 'astro:content'
import type { Lang } from './i18n'

export type Doc = CollectionEntry<'docs'>

/** The page's slug within its language: '' for the index. */
export function slugOf(doc: Doc): string {
  const slug = doc.id.split('/').slice(1).join('/')
  return slug === 'index' ? '' : slug
}

/** A language's documentation pages, in sidebar order. */
export async function docsFor(lang: Lang): Promise<Doc[]> {
  const all = await getCollection('docs', (d) => d.id.startsWith(`${lang}/`))
  return all.sort((a, b) => a.data.order - b.data.order)
}

/** getStaticPaths for one language's docs route. */
export async function docsPaths(lang: Lang) {
  const pages = await docsFor(lang)
  return pages.map((doc) => ({ params: { slug: slugOf(doc) || undefined }, props: { doc, pages } }))
}
