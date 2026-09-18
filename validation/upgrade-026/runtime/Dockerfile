FROM postgres:18-alpine
LABEL com.codex.local-task="sub2api-upgrade026-20260918"
WORKDIR /app
COPY --chmod=0555 sub2api /app/sub2api
COPY --chown=1000:1000 resources /app/resources
RUN mkdir -p /app/data && chown 1000:1000 /app/data
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/app/sub2api"]
