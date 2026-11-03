FROM node:24-alpine AS web
RUN npm install -g pnpm@11.20.0
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /vrac ./cmd/vrac

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -S vrac && adduser -S -G vrac vrac && mkdir /data && chown vrac:vrac /data
WORKDIR /app
COPY --from=server /vrac /usr/local/bin/vrac
COPY --from=web /src/web/dist /app/web
USER vrac
EXPOSE 5235
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/vrac"]
