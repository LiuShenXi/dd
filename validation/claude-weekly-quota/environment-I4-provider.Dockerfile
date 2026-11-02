FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
COPY provider /provider
USER 1000:1000
EXPOSE 8081
LABEL com.codex.local-task="claude-weekly-quota" com.monas.stage="I4-synthetic-provider-no-forwarding"
ENTRYPOINT ["/provider"]
