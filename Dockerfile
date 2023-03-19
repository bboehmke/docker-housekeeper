FROM golang:1.20

COPY . /src/
WORKDIR /src/

RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /app .

# use edge image for higher client versions
FROM alpine:edge

# install pg client for pg_dump
RUN apk add --no-cache postgresql-client

# copy app from build image
COPY --from=0 /app /app

VOLUME /backup/
CMD "/app"