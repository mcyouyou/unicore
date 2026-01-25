FROM golang:1.25 AS builder
WORKDIR /workspace
COPY . .
RUN go env -w  GOPROXY=https://goproxy.cn,direct
ARG BUILD_TIME
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.buildTime=$BUILD_TIME" -o scheduler cmd/main.go

FROM alpine
COPY --from=builder /workspace/scheduler /scheduler
ENTRYPOINT ["/scheduler --leader-elect=false --config=/etc/kube-scheduler/config.yaml"]