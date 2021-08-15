FROM golang:1.16.6 as builder

WORKDIR /app
ADD . /app

RUN go get -d -v ./...

# Statically compile our app for use in a distroless container
RUN CGO_ENABLED=0 go build -v -ldflags="-w -s \
    -X miniprow.buildDate=$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
    -o "miniprow" "./actions/merge/"

# FROM gcr.io/distroless/static
FROM alpine:latest

COPY --from=builder /app/miniprow /miniprow

ENTRYPOINT ["/miniprow"]

