FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

ENV CGO_ENABLED=0
WORKDIR /src

# Install build-time dependencies for the Vue frontend and stuffbin packing.
RUN apt-get update \
  && apt-get install -y --no-install-recommends curl nodejs npm make git ca-certificates tzdata \
  && npm install -g corepack \
  && corepack enable \
  && rm -rf /var/lib/apt/lists/*

# Pre-fetch Go modules and JS dependencies for caching efficiency.
COPY go.mod go.sum ./
RUN go mod download

# Copy static directory first to ensure it exists for frontend postinstall script
COPY static/ ./static/

COPY frontend/package.json frontend/yarn.lock ./frontend/
COPY frontend/email-builder/package.json frontend/email-builder/yarn.lock ./frontend/email-builder/
RUN cd frontend && yarn install --frozen-lockfile \
  && cd email-builder && yarn install --frozen-lockfile

# Copy the remaining project files and build the binary with bundled assets.
COPY . .
RUN make dist

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata shadow su-exec

WORKDIR /listmonk

COPY --from=builder /src/listmonk ./listmonk
COPY config.toml.sample config.toml
COPY docker-entrypoint.sh /usr/local/bin/

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 9000

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["./listmonk"]
