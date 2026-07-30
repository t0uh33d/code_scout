# Generated Templ files and the compiled Tailwind CSS are committed, so the
# build is a plain Go build with no Node toolchain needed.
FROM golang:1.24-alpine AS build

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src

# Dependencies first, so this layer stays cached until go.mod or go.sum changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary that runs on a bare image.
# -w -s strips debug info, which roughly halves the binary size.
RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s -X 'main.BuildTime=${BUILD_TIME}' -X 'main.BranchName=${VERSION}' -X 'main.CommitHash=${COMMIT}'" \
    -o /out/code_scout main.go


FROM alpine:3.20

# ca-certificates is required for TLS connections to managed databases.
# tzdata keeps time formatting correct outside UTC.
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 codescout

COPY --from=build /out/code_scout /usr/local/bin/code_scout

USER codescout
EXPOSE 24275

HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:24275/healthz > /dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/code_scout"]
