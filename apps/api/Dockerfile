# Build context is apps/api (Render: Root Directory = apps/api).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
USER app
COPY --from=build /out/api /usr/local/bin/api
# Config comes from environment variables (no .env in the image).
ENV HOST=0.0.0.0
EXPOSE 10000
ENTRYPOINT ["api"]
