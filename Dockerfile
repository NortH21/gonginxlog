# syntax=docker/dockerfile:1

# Build natively on the host platform and cross-compile for the target
# platform - Go cross-compiles cheaply, so this avoids QEMU-emulating the
# compiler itself under multi-arch buildx builds.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE} -X main.builtBy=docker" \
    -o /out/gonginxlog .

# No shell, no libc, just the static binary - gonginxlog only ever reads
# local files, so there's nothing else it needs at runtime.
FROM scratch
COPY --from=build /out/gonginxlog /gonginxlog
ENTRYPOINT ["/gonginxlog"]
