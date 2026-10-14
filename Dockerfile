FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /collector ./cmd/collector
FROM debian:bookworm-slim
RUN useradd -m -u 10001 agent
WORKDIR /app
COPY --from=build /collector /usr/local/bin/identity-monitor-agent
COPY configs ./configs
RUN chown -R agent:agent /app
USER agent
ENTRYPOINT ["identity-monitor-agent"]
CMD ["demo"]
