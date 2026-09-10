import { readFile, copyFile } from 'node:fs/promises'
import { constants } from 'node:fs'
import { resolve, basename } from 'node:path'
import { createHash } from 'node:crypto'

const [sourceArg, targetArg] = process.argv.slice(2)
if (!sourceArg || !targetArg) throw new Error('Expected captured assets and final assets directory')
const source = resolve(sourceArg)
const target = resolve(targetArg)
const manifest = JSON.parse(await readFile(resolve(source, 'capture-manifest.json'), 'utf8'))
for (const item of manifest) {
  if (!/^\/assets\/[a-zA-Z0-9_.-]+$/.test(item.path)) throw new Error('Unexpected static asset path')
  const from = resolve(source, basename(item.path))
  const to = resolve(target, basename(item.path))
  const bytes = await readFile(from)
  if (createHash('sha256').update(bytes).digest('hex') !== item.sha256) throw new Error('Captured asset integrity failed')
  try { await copyFile(from, to, constants.COPYFILE_EXCL) } catch (error) {
    if (error.code !== 'EEXIST' || !(await readFile(to)).equals(bytes)) throw error
  }
}
console.log(`Retained ${manifest.length} exact old assets in the new embedded frontend`)
