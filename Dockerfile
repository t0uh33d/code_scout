# Generated Templ files and the compiled Tailwind CSS are committed, so the
# build is a plain Go build with no Node toolchain needed.
FROM golang:1.25-alpine AS build

# There is deliberately no VERSION arg. The version is a constant in
# app/version.go, so an image cannot claim a version its binary does not have.
# There used to be one, and it was wired into app.BranchName.
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src

# Dependencies first, so this layer stays cached until go.mod or go.sum changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary that runs on a bare image.
# -w -s strips debug info, which roughly halves the binary size.
# Build "." and not "main.go": naming the file compiles only that file, so any
# other file in package main (reset_password.go) goes missing at link time.
RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s \
      -X 'github.com/getcodescout/code_scout/app.Commit=${COMMIT}' \
      -X 'github.com/getcodescout/code_scout/app.BuildTime=${BUILD_TIME}'" \
    -o /out/code_scout .


FROM alpine:3.20

# What links the published package back to this repository. Without it the
# image sits in the org's package list unattached to any source, which for a
# self-hosted tool people are about to run on their own machines is exactly the
# wrong impression. The registry reads this label and nothing else.
LABEL org.opencontainers.image.source="https://github.com/getcodescout/code_scout"
LABEL org.opencontainers.image.description="Self-hosted logging and network inspection for Flutter apps"
LABEL org.opencontainers.image.licenses="MIT"

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
