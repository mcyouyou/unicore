FROM golang:1.25 AS builder
WORKDIR /workspace
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o scheduler cmd/main.go

FROM alpine
COPY --from=builder /workspace/scheduler /scheduler
ENTRYPOINT ["/scheduler --leader-elect=false --config=/etc/kube-scheduler/config.yaml"]
