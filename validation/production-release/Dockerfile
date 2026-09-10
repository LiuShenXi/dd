FROM sub2api:0.2.1-refundfix-36266f512776
ARG SOURCE_REVISION
ARG BINARY_SHA256
LABEL org.opencontainers.image.revision="${SOURCE_REVISION}"
LABEL sub2api.binary.sha256="${BINARY_SHA256}"
LABEL sub2api.release="carpool-production-20260908"
COPY --chmod=0755 sub2api /app/sub2api
COPY --chmod=0755 carpool-release /app/carpool-release
COPY resources /app/resources
