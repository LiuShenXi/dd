import { execFileSync } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'

const [output] = process.argv.slice(2)
if (!output) throw new Error('Expected private manifest output path')
const paths = execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', 'backend', 'frontend']).toString().split('\0').filter(Boolean).sort()
const files = []
const digest = createHash('sha256')
for (const path of paths) {
  const sha256 = createHash('sha256').update(await readFile(path)).digest('hex')
  files.push({ path, sha256 })
  digest.update(path + '\0' + sha256 + '\n')
}
const result = { source_sha256: digest.digest('hex'), base_commit: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), files }
await writeFile(output, JSON.stringify(result,null,2), { flag: 'wx', mode: 0o600 })
console.log(`SOURCE_SHA256=${result.source_sha256} FILES=${files.length}`)
