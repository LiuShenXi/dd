FROM postgres:18.1-alpine3.23@sha256:aa6eb304ddb6dd26df23d05db4e5cb05af8951cda3e0dc57731b771e0ef4ab29 AS pg-client

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS runtime-base
RUN apk add --no-cache ca-certificates tzdata su-exec libpq zstd-libs lz4-libs krb5-libs libldap libedit \
    && addgroup -g 1000 sub2api \
    && adduser -u 1000 -G sub2api -s /bin/sh -D sub2api \
    && mkdir -p /app/data && chown sub2api:sub2api /app/data
COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql
COPY --from=pg-client /usr/local/lib/libpq.so.5* /usr/local/lib/
WORKDIR /app

FROM runtime-base AS local-r1
ARG SOURCE_SHA256
ARG REVISION
ARG BUILD_DATE
LABEL org.opencontainers.image.title="Monas Claude weekly quota I3 current receipt compatible R1" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      com.codex.local-task="claude-weekly-quota" \
      com.monas.source-sha256="${SOURCE_SHA256}" \
      com.monas.claude-quota-stage="I3-current-receipt-R1" \
      com.monas.claude-quota-paid-order-recovery="requires-I5-I6-revalidation" \
      com.monas.claude-quota-activation="unsupported" \
      com.monas.claude-quota-recovery="subscription-quota-receipt-v1-and-existing-GPT"
ENV SUBSCRIPTION_QUOTA_ISSUE_ENABLED=false \
    SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=false \
    SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=true
COPY --chown=sub2api:sub2api sub2api /app/sub2api
COPY --chown=sub2api:sub2api carpool-release /app/carpool-release
COPY --chown=sub2api:sub2api resources /app/resources
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN sed -i 's/\r$//' /app/docker-entrypoint.sh && chmod 0755 /app/docker-entrypoint.sh /app/sub2api /app/carpool-release
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://localhost:${SERVER_PORT:-8080}/health || exit 1
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]
