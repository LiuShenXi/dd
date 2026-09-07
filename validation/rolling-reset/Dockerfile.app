FROM postgres:18-alpine

WORKDIR /app
COPY --chmod=0555 validation/rolling-reset/.runtime/app-build/sub2api /app/sub2api
COPY --chown=1000:1000 backend/resources /app/resources
RUN mkdir -p /app/data && chown 1000:1000 /app/data

USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/app/sub2api"]

