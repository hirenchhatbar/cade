FROM golang:1.27.3 AS dev
# World-writable caches so the container can run as the host user (no root-owned files in the repo).
RUN mkdir -p /cache/go-build /cache/mod && chmod -R 777 /cache
ENV GOCACHE=/cache/go-build GOMODCACHE=/cache/mod
WORKDIR /src

FROM dev AS build
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cade ./cmd/cade

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cade /cade
ENTRYPOINT ["/cade"]
