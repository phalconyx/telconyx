# syntax=docker/dockerfile:1.7

# ---- build stage ----
FROM golang:1.22-alpine AS build
WORKDIR /src

# Cache go modules
COPY go.mod go.sum* ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/telconyx \
    ./cmd/telconyx

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telconyx /usr/local/bin/telconyx

EXPOSE 9090
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/telconyx", "serve"]
