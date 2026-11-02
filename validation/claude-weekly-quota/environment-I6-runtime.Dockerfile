# Production R0 provenance is checked by office-build-I6.py before and after.
FROM sub2api:2.8.13-monas-bps.13
ARG SOURCE_SHA256
ARG REVISION
ARG BUILD_DATE
ARG QUOTA_STAGE=I6-isolated-candidate
LABEL org.opencontainers.image.title="Monas Claude quota I6 candidate" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      com.codex.local-task="claude-weekly-quota" \
      com.monas.source-sha256="${SOURCE_SHA256}" \
      com.monas.claude-quota-stage="${QUOTA_STAGE}" \
      com.monas.claude-quota-activation="local-synthetic-only" \
      com.monas.runtime-r0-oci-index="sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677"
# Both artifacts default to compatible refusal. Enabling R2 is explicit config.
ENV SUBSCRIPTION_QUOTA_ISSUE_ENABLED=false \
    SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=false \
    SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=true \
    DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=3
COPY --chown=1000:1000 --chmod=0755 sub2api /app/sub2api
COPY --chown=1000:1000 --chmod=0755 carpool-release /app/carpool-release
COPY --chown=1000:1000 resources /app/resources
