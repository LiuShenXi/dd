import { mkdir, writeFile, readFile } from 'node:fs/promises'
import { resolve, basename } from 'node:path'
import { createHash } from 'node:crypto'

const [originArg, outputArg] = process.argv.slice(2)
const origin = new URL(originArg)
if (origin.origin !== 'https://sub2api.monasapi.com' || !outputArg) throw new Error('Expected the existing production origin and a private output directory')
const output = resolve(outputArg)
await mkdir(output, { recursive: true, mode: 0o700 })
const queue = [new URL('/', origin)]
const seen = new Set()
const manifest = []
while (queue.length) {
  const url = queue.shift()
  if (seen.has(url.href)) continue
  seen.add(url.href)
  if (seen.size > 3000) throw new Error('Unexpectedly large static graph')
  const response = await fetch(url, { redirect: 'error', signal: AbortSignal.timeout(20000) })
  if (!response.ok) throw new Error(`Static read failed: ${url.pathname} ${response.status}`)
  const bytes = Buffer.from(await response.arrayBuffer())
  const type = response.headers.get('content-type') ?? ''
  if (url.pathname !== '/') {
    if (type.includes('text/html')) throw new Error(`Asset returned HTML: ${url.pathname}`)
    const target = resolve(output, basename(url.pathname))
    try { await writeFile(target, bytes, { mode: 0o600, flag: 'wx' }) } catch(error) {
      if (error.code !== 'EEXIST' || !(await readFile(target)).equals(bytes)) throw error
    }
    manifest.push({ path: url.pathname, sha256: createHash('sha256').update(bytes).digest('hex'), bytes: bytes.length })
  }
  if (!type.includes('javascript') && !type.includes('text/css') && url.pathname !== '/') continue
  const text = bytes.toString('utf8')
  // Vite's dependency map includes lazy chunks as quoted static asset paths.
  const references = text.matchAll(/["'`(]((?:\.?\.?\/|\/)?(?:assets\/)?[a-zA-Z0-9_./-]+\.(?:js|css|png|webp|svg|woff2?))(?:["'`)])/g)
  for (const [, reference] of references) {
    const child = new URL(reference, reference.startsWith('assets/') ? origin : url)
    if (child.origin === origin.origin && child.pathname.startsWith('/assets/') && /-[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9]+$/.test(child.pathname) && !seen.has(child.href)) queue.push(child)
  }
}
await writeFile(resolve(output, 'capture-manifest.json'), JSON.stringify(manifest, null, 2), { mode: 0o600, flag: 'wx' })
console.log(`Captured ${manifest.length} production assets; bytes=${manifest.reduce((sum,item)=>sum+item.bytes,0)}`)
