#部署bookinfo.yaml应用
kubectl apply -f bookinfo.yaml

kubectl get services

kubectl get pods

#验证是否在集群中运行
kubectl exec "$(kubectl get pod -l app=ratings -o jsonpath='{.items[0].metadata.name}')" -c ratings -- curl -sS productpage:9080/productpage | grep -o "<title>.*</title>"

