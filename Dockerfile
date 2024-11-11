# syntax=docker/dockerfile:1
FROM golang:1.23

COPY . /src/
WORKDIR /src/

RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /docker_housekeeper .

# use edge image for higher client versions
FROM alpine:edge

# install pg client for pg_dump
RUN apk add --no-cache postgresql-client

# copy app from build image
COPY --from=0 /docker_housekeeper /docker_housekeeper

HEALTHCHECK --timeout=10s --start-period=60s --start-interval=2s CMD /docker_housekeeper healthcheck

VOLUME /backup/
CMD "/docker_housekeeper"
