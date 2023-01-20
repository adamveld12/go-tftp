FROM golang:alpine as build

COPY . /go/src

WORKDIR /go/src

RUN go build -v -o tftp-server /go/src/

FROM alpine

LABEL org.opencontainers.image.source="https://github.com/adamveld12/go-tftp"
LABEL org.opencontainers.image.description="tftp server implementation"
LABEL org.opencontainers.image.licenses="Apache 2.0"

COPY --from=build /go/src/tftp-server /bin/tftp-server

EXPOSE 69/udp

VOLUME /var/tftp-data

ENTRYPOINT ["/bin/tftp-server"]
CMD ["-directory=/var/tftp-data", "-port=69", "-address=", "-verbose"]
