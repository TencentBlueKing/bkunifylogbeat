FROM golang:1.16 as builder
ARG VERSION=7.3.1
WORKDIR /workspace
COPY . .
RUN set -x; echo ${VERSION} ; CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --ldflags="-s -w -X main.version=${VERSION}" -o bkunifylogbeat main.go

FROM centos:7
WORKDIR /data
COPY --from=builder /workspace/bkunifylogbeat /bin/bkunifylogbeat
COPY bkunifylogbeat_main.yml /etc/bkunifylogbeat.conf
RUN yum install -y strace lsof
ENTRYPOINT ["/bin/bkunifylogbeat", "-c", "/etc/bkunifylogbeat.conf"]
