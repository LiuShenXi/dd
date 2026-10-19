FROM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d
RUN apk add --no-cache gcc musl-dev
LABEL com.codex.local-task="claude-weekly-quota" \
      com.codex.test-role="linux-build-race"
ENV GOTOOLCHAIN=local GOMAXPROCS=4 CGO_ENABLED=1
WORKDIR /workspace/backend
CMD ["sleep", "infinity"]
