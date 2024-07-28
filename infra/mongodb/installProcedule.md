## 部署 mongodb-operator 的步骤
> 之后会由 unicore-operator 自动完成
```sh
kubectl create ns unicore-mongo  
kubectl apply -f mongo.yaml -n unicore-mongo  
kubectl apply -k rbac -n unicore-mongo
kubectl create -f manager.yaml -n unicore-mongo
kubectl apply -f mongodb_cr.yaml -n unicore-mongo

kubectl create namespace cert-manager
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install \
  cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --version v1.3.1 \
  --set installCRDs=true
```