# I4 portable evidence index

I4 is accepted; production and I5/I6 remain untouched. The final scope is commit
and push of this isolated feature branch, followed by company continuation.

Read `coordinator-I4-review.md`, `coordinator-current-status.md`,
`coordinator-resume-state.md`, `I4-receipt.md`, `I4-F4-key-projection-receipt.md`
and `I4-next-machine-handoff.md` first. The acceptance tracker deliberately retains
historical iteration scope and incomplete final-release requirements.

Final F4 source manifest remains readable outside the archive:
`I4-main-F4-key-projection-source.json`, source
`9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`.
The final image is `sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9`.
Final F4 Windows/race/native PG/HTTP logs and receipts, final frontend component
test output, actual browser proof and independent review are directly readable.
The current task metadata points to the coordinator's current status rather than
the original architecture planning state. Original contract text is preserved.

`I4-diagnostics-archive.zip` is committed alongside this index. It contains254
immutable plaintext originals (79,922,745bytes), including prior failed runs and
duplicate manifests, compressed to20,142,655bytes. SHA256:
`35dfedf8131bfb67d4edd2b2e31d3956adaa9668dc3bff68f5ea0d13c4e5cad0`.
`I4-diagnostics-archive-manifest.json` lists each filename/length/SHA256. Every
entry was verified after compression; `I4-verify-evidence.py --extract` was also
executed successfully and recovered all originals byte-for-byte. Ignored raw
files are preserved locally, not deleted. On another machine the committed ZIP
is sufficient to recover them; no private archive path is needed.

```powershell
python validation/claude-weekly-quota/I4-verify-evidence.py
python validation/claude-weekly-quota/I4-verify-evidence.py --extract
```

Prior I3 diagnostics have a separate committed archive and manifest. Failed F1/F2
and diagnostic evidence retains its own status and original source identity;
restoration does not make a failed run pass. Some raw references in receipts
resolve after extraction. See `I4-failure-correction-index.json` for failure
classification and `coordinator-I4-F2-F3-test-only-bridge.json` for test-only reuse.

Credential checks cover old ignored plaintext diagnostics and changed files,
including exact comparisons against task-owned private-fixture credentials read
only into scanner memory. Only file/category findings are serialized. Public
config example placeholders are classified separately; no matching secret value
is printed. Browser owner confirmed Key inputs stayed password-masked and only
synthetic accounts were used. Final scan reports accompany the commit.

Excluded resources are documented in `I4-local-external-resources-excluded.json`.
Private fixture JSON, passwords, Keys/JWTs, database dumps, Docker volumes and
temporary exe/image archives are not included in this ZIP or commit. New company
testing generates fresh synthetic fixtures; do not copy the old live environment.
