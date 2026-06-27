# syntax=docker/dockerfile:1
#
# Kamailio-Go container image.
#
# Single static binary; the role (pcscf|icscf|scscf|all) is selected at
# runtime via `--role`, so one image serves every IMS deployment shape.

# ---- builder ----
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: modernc.org/sqlite is pure Go, so the binary is fully
# static and runs on any base image without libc concerns.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -ldflags "-s -w" \
        -o /out/kamailio ./cmd/kamailio

# ---- runtime ----
FROM alpine:3.20
# ca-certificates for outbound TLS (e.g. Diameter/TLS, RPC clients).
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 kamailio

COPY --from=builder /out/kamailio /usr/local/bin/kamailio
# Built-in example configs (may be overridden by a volume mount).
COPY configs/ /etc/kamailio/configs/

USER kamailio
WORKDIR /etc/kamailio

# SIP UDP/TCP. Each role maps host 5060/5061/5062 in docker-compose.
EXPOSE 5060/udp 5060/tcp

# ENTRYPOINT is the binary; CMD is the default subcommand so operators can
# override easily, e.g. `docker run kamailio-go check-config -f ...`.
ENTRYPOINT ["kamailio"]
CMD ["run"]
