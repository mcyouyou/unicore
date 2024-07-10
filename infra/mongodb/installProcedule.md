## 部署 mongodb-operator 的步骤
> 之后会由 unicore-operator 自动完成
```sh
kubectl create ns unicore-mongo  
kubectl apply -f mongo.yaml -n unicore-mongo  
kubectl apply -k rbac -n unicore-mongo
kubectl create -f manager.yaml -n unicore-mongo

```