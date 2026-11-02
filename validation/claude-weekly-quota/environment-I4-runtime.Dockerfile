# The build script verifies this local tag's configuration digest and its 20
# RootFS diffIDs against coordinator-r0-provenance.json before and after build.
FROM sub2api:2.8.13-monas-bps.13
ARG SOURCE_SHA256
ARG REVISION
ARG BUILD_DATE
LABEL org.opencontainers.image.title="Monas Claude quota I4 isolated candidate" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      com.codex.local-task="claude-weekly-quota" \
      com.monas.source-sha256="${SOURCE_SHA256}" \
      com.monas.claude-quota-stage="I4-isolated-candidate" \
      com.monas.claude-quota-activation="local-synthetic-only" \
      com.monas.claude-quota-paid-order-recovery="requires-I5-I6-revalidation" \
      com.monas.runtime-r0-oci-index="sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677" \
      com.monas.runtime-r0-config="sha256:7e6d174ece5f28faf73680802bd52ff0b34a30bd59600125e9aaf53e843aa7a6"
ENV SUBSCRIPTION_QUOTA_ISSUE_ENABLED=false \
    SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=false \
    SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=true \
    DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=3
COPY --chown=1000:1000 --chmod=0755 sub2api /app/sub2api
COPY --chown=1000:1000 --chmod=0755 carpool-release /app/carpool-release
COPY --chown=1000:1000 resources /app/resources
# Keep the verified R0 Alpine 3.21 runtime, PostgreSQL clients and entrypoint.
