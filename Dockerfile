FROM docker.io/library/golang:1.22 AS build-env
RUN apt update && apt install -y musl-tools
WORKDIR /app
COPY ./ /app
RUN go mod tidy && CC=musl-gcc go build -o bin/app main.go

FROM docker.io/library/alpine:3.19
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=build-env /app/bin/app /app/app
COPY --from=build-env /app/index.html /app/index.html
ENTRYPOINT ["/app/app"]