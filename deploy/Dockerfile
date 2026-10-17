FROM golang:1.27.1 AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /trip-service ./cmd/trip-service

FROM scratch
COPY --from=build /trip-service /trip-service
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/trip-service"]
