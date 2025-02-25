CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o daemon .
docker build -t unicore-daemon .
kind load docker-image unicore-daemon -n dev
kubectl delete daemonset daemon -n unicore
kubectl apply -f unicore_daemon.yaml
sleep 3
kubectl get po -n unicore