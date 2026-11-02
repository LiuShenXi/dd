# I3 evidence packaging

The reviewable files retain the final F3 source manifest, functional JSONL,
receipts, build/inspect logs, scripts, stage receipts and coordinator reviews.
All final source checks bind
`c9d25ce07053fba7642a8eb4473288b88877d4bec62472df8cf2180ef4b7eebe`.

`I3-diagnostics-archive.zip` preserves superseded/debug receipts and JSONL,
owner-slice raw results, and duplicate before/after source manifests, including
the duplicate F3 manifests referenced by final receipts. Each entry retains
its original basename. Extract to a separate review directory when reading
these archived references; the single directly readable complete F3 manifest
is `I3-pg-F3-source.json`. No raw original was deleted locally.

- Archive SHA256: `4f6db1a78ff16754862262e00d508bfb93ce4e48a90d5d135930469003d08f61`.
- Archive size: 17,027,807 bytes; 175 entries; 68,095,069 uncompressed bytes.
- `I3-diagnostics-archive-manifest.json` lists every entry's size and SHA256.
- `I3-archive-diagnostics.ps1` records the selection and packaging procedure;
  it verified every compressed entry's hash before ignoring its raw original.
  Existing archives are immutable and the script refuses to overwrite them.

The local `.gitignore` names only those archived originals. It does not ignore
future stages, remove files, or alter the business source hash. No executable,
credentials, production connection configuration or database dump is included
in this archive. Diagnostic failures remain diagnostic; the final F3 results
are identified by `I3-receipt.md` and `I3-pg-F3-final-receipt.json`.
