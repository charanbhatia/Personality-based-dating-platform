# Build on the host's native architecture and let Go cross-compile to the
# target. CGO is off, so this needs no emulation and keeps builds fast when the
# deployment target does not match the machine doing the building.
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGET=server
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -o /out/app ./cmd/${TARGET}

FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 10001 app

COPY --from=build /out/app /usr/local/bin/app

USER app
EXPOSE 8080
ENTRYPOINT ["app"]
