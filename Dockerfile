ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/sigint ./cmd/sigint
RUN mkdir -p /out/var

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/sigint /usr/local/bin/sigint
COPY --from=build --chown=nonroot:nonroot /out/var /app/var
COPY --chown=nonroot:nonroot examples/compose.config.yaml /app/config/config.yaml

EXPOSE 8920

ENV SIGINT_CONFIG=/app/config/config.yaml

ENTRYPOINT ["/usr/local/bin/sigint"]
CMD ["server", "start", "--config", "/app/config/config.yaml", "--host", "0.0.0.0", "--port", "8920"]
