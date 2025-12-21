#!/usr/bin/env bash
set -euo pipefail

NS="unicore"
CM="reservation"

# 计算10分钟后的Unix时间戳
EXPIRE_TS=$(( $(date -u +%s) + 600 ))

echo "==> 准备 namespace"
kubectl create ns ${NS} --dry-run=client -o yaml | kubectl apply -f -

echo "==> 创建 reservation ConfigMap（kind-worker 预留 2c）"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${CM}
  namespace: ${NS}
data:
  kind-worker: |
    [
      {
        "pod": "reserved-pod",
        "namespace": "${NS}",
        "cpu": 2000,
        "mem": 0,
        "expire": ${EXPIRE_TS}
      }
    ]
EOF

sleep 3

echo
echo "==> Step 1: 打满 kind-worker2 (3c)"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: fill-worker1
  namespace: ${NS}
spec:
  schedulerName: unicore-scheduler  # 指定自定义调度器
  containers:
  - name: c
    image: busybox
    imagePullPolicy: Never
    command: ["sh", "-c", "sleep 3600"]
    resources:
      requests:
        cpu: "3"
EOF

echo "==> Step 2: 打满 kind-worker3 (3c)"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: fill-worker2
  namespace: ${NS}
spec:
  schedulerName: unicore-scheduler  # 指定自定义调度器
  containers:
  - name: c
    image: busybox
    imagePullPolicy: Never
    command: ["sh", "-c", "sleep 3600"]
    resources:
      requests:
        cpu: "3"
EOF

kubectl wait --for=condition=Ready pod/fill-worker1 -n ${NS} --timeout=60s
kubectl wait --for=condition=Ready pod/fill-worker2 -n ${NS} --timeout=60s

echo
echo "==> Step 3: 下发普通 pod（3c），理论上不应当有空闲"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: normal-pod
  namespace: ${NS}
spec:
  schedulerName: unicore-scheduler  # 指定自定义调度器
  containers:
  - name: c
    image: busybox
    imagePullPolicy: Never
    command: ["sh", "-c", "sleep 3600"]
    resources:
      requests:
        cpu: "3"
EOF

sleep 5

PHASE=$(kubectl get pod normal-pod -n ${NS} -o jsonpath='{.status.phase}')
NODE=$(kubectl get pod normal-pod -n ${NS} -o jsonpath='{.spec.nodeName}')

echo "normal-pod phase=${PHASE}, node=${NODE:-<none>}"

if [[ "${PHASE}" != "Pending" ]]; then
  echo "❌ 错误：normal-pod 不应被调度"
  exit 1
fi
echo "✅ 正确：预留资源阻止普通 pod 调度"

echo
echo "==> Step 4: 下发被预留 pod（2c）"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: reserved-pod
  namespace: ${NS}
spec:
  schedulerName: unicore-scheduler  # 指定自定义调度器
  containers:
  - name: c
    image: busybox
    imagePullPolicy: Never
    command: ["sh", "-c", "sleep 3600"]
    resources:
      requests:
        cpu: "2"
EOF

kubectl wait --for=condition=Ready pod/reserved-pod -n ${NS} --timeout=60s

NODE2=$(kubectl get pod reserved-pod -n ${NS} -o jsonpath='{.spec.nodeName}')
echo "reserved-pod node=${NODE2}"

if [[ "${NODE2}" != "kind-worker" ]]; then
  echo "❌ 错误：reserved-pod 没有使用预留节点"
  exit 1
fi
echo "✅ 正确：reserved-pod 使用了被预留节点"

echo
echo "==> Step 5: 再下发普通 pod（1.5c）"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: after-reserve-pod
  namespace: ${NS}
spec:
  schedulerName: unicore-scheduler  # 指定自定义调度器
  containers:
  - name: c
    image: busybox
    imagePullPolicy: Never
    command: ["sh", "-c", "sleep 3600"]
    resources:
      requests:
        cpu: "1.5"
EOF

kubectl wait --for=condition=Ready pod/after-reserve-pod -n ${NS} --timeout=60s
NODE3=$(kubectl get pod after-reserve-pod -n ${NS} -o jsonpath='{.spec.nodeName}')

echo "after-reserve-pod node=${NODE3}"
echo "🎉 强化预占测试完成"
