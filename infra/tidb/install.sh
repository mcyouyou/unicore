kubectl create -f https://raw.githubusercontent.com/pingcap/tidb-operator/v1.6.0/manifests/crd.yaml
helm repo add pingcap https://charts.pingcap.org/
kubectl create namespace tidb-admin
helm install --namespace tidb-admin tidb-operator pingcap/tidb-operator --version v1.6.0 \
    --set operatorImage=registry.cn-beijing.aliyuncs.com/tidb/tidb-operator:v1.6.0 \
    --set tidbBackupManagerImage=registry.cn-beijing.aliyuncs.com/tidb/tidb-backup-manager:v1.6.0 \
    --set scheduler.kubeSchedulerImageName=registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler --wait

# wait for operator ok

kubectl apply -f cluster.yaml
kubectl apply -f dashboard.yaml
kubectl apply -f monitor.yaml