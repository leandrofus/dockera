// The latest release, read once at build time: the Pages workflow rebuilds
// the site whenever a release is published.
export const REPO = process.env.RELEASE_REPO || 'leandrofus/ducktainer'

export interface Asset {
  name: string
  url: string
}

export interface Release {
  tag: string
  assets: Asset[]
}

export async function latestRelease(): Promise<Release | null> {
  try {
    const headers: Record<string, string> = { Accept: 'application/vnd.github+json' }
    if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`
    const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, { headers })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const rel = await res.json()
    return {
      tag: rel.tag_name,
      assets: rel.assets.map((a: { name: string; browser_download_url: string }) => ({ name: a.name, url: a.browser_download_url })),
    }
  } catch (e) {
    console.warn(`latest release of ${REPO}: ${e}; the page links to the releases list instead`)
    return null
  }
}

export type System = 'dnf' | 'apt' | 'linux' | 'darwin' | 'windows'
export type Arch = 'amd64' | 'arm64'

/** The commands that download and install a release on a system. */
export function installCommands(rel: Release, sys: System, arch: Arch): string[] | null {
  const find = (re: RegExp) => rel.assets.find((a) => re.test(a.name))
  let a: Asset | undefined
  switch (sys) {
    case 'dnf':
      a = find(new RegExp(`\\.${arch === 'arm64' ? 'aarch64' : 'x86_64'}\\.rpm$`))
      return a ? [`curl -fLO ${a.url}`, `sudo dnf install ./${a.name}`] : null
    case 'apt':
      a = find(new RegExp(`_${arch}\\.deb$`))
      return a ? [`curl -fLO ${a.url}`, `sudo apt install ./${a.name}`] : null
    case 'windows':
      a = find(new RegExp(`^ducktainer-windows-${arch}\\.exe$`))
      return a ? [`curl.exe -fLo ducktainer.exe ${a.url}`, '.\\ducktainer.exe serve'] : null
    default:
      a = find(new RegExp(`^ducktainer-${sys}-${arch}$`))
      return a ? [`curl -fLo ducktainer ${a.url}`, 'chmod +x ducktainer && ./ducktainer serve'] : null
  }
}
