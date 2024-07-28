#!/bin/bash

# 获取所有 PVC 的名称
pvc_names=$(kubectl get pvc --output=jsonpath='{.items[*].metadata.name}')

# 循环删除每个 PVC
for pvc_name in $pvc_names; do
    kubectl delete pvc $pvc_name
done

# 获取所有具有 Retain 回收策略的 PV 的名称
pv_names=$(kubectl get pv --output=jsonpath='{.items[?(@.spec.persistentVolumeReclaimPolicy=="Retain")].metadata.name}')

# 循环删除每个 PV
for pv_name in $pv_names; do
    kubectl delete pv $pv_name
done