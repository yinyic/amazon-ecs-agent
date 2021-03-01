# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang as build
ENV GO111MODULE=auto

# WORKDIR /src/
ADD . /go/src/

RUN go build -o ./bin/health ./src
RUN ls ./bin

FROM scratch
COPY --from=build /go/bin/health /
CMD ["/health"]
