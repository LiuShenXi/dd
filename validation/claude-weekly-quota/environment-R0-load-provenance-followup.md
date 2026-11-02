The initial local import receipt, environment-R0-load-20261019T1710Z-mismatch.json,
correctly records that Docker25 did not find the production OCI index digest as
a local classic image ID. It is retained as the initial observation.

The coordinator subsequently verified the exported OCI index, linux/amd64
manifest and configuration graph, all20RootFS diffIDs, revision, and the actual
server binary hash in coordinator-r0-provenance.json. Production Docker29 reports
index fb5ecafe..., whereas the local classic store reports config7e6d174e....
They identify the same verified runtime and executable. The probe used network
none and did not start the application.

The I4 build now starts from this verified local R0 tag, checks its configuration
ID before/after build and retains all20base diffIDs. It only replaces application
executables and resources. Early I3 local Alpine3.23 images remain distinct;
they are not the final v2/I5 rollback carrier.
