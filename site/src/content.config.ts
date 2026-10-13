import { defineCollection, z } from 'astro:content'
import { glob } from 'astro/loaders'

// Documentation pages: src/content/docs/<lang>/<page>.md, ordered by `order`.
const docs = defineCollection({
  // ids are the paths without extension: en/index, es/install, …
  loader: glob({ pattern: '**/*.md', base: './src/content/docs', generateId: ({ entry }) => entry.replace(/\.md$/, '') }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    order: z.number(),
  }),
})

export const collections = { docs }
