FROM golang:1.21-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGET=server
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/${TARGET}

FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 10001 app

COPY --from=build /out/app /usr/local/bin/app

USER app
EXPOSE 8080
ENTRYPOINT ["app"]
