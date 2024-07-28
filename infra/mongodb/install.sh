kubectl create ns unicore-mongo
kubectl apply -f mongo.yaml -n unicore-mongo
kubectl apply -k rbac -n unicore-mongo
kubectl create -f manager.yaml -n unicore-mongo
kubectl apply -f mongodb_cr.yaml -n unicore-mongo