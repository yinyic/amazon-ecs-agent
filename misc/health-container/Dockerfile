# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang as build
ENV GO111MODULE=auto

# WORKDIR /src/
ADD . /go/src/

RUN go get github.com/containerd/containerd
RUN go get github.com/docker/docker/api/types
RUN go get github.com/docker/docker/client

RUN go build -o ./bin/health ./src
RUN ls /go/bin/

# FROM scratch
FROM golang
WORKDIR /
COPY --from=build /go/bin/health /
RUN ls
CMD ["/health"]
