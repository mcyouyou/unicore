export PATH="$PATH:./istio-1.22.2/bin"
FILE=./samples/bookinfo.yaml

#部署应用
#kubectl create deployment hello-minikube --image=kicbase/echo-server:1.0
#kubectl expose deployment hello-minikube --type=NodePort --port=8080

#kubectl get services hello-minikube


#部署bookinfo.yaml应用
kubectl apply -f $FILE

#kubectl get services

#kubectl get pods

#验证是否在集群中运行
kubectl exec "$(kubectl get pod -l app=ratings -o jsonpath='{.items[0].metadata.name}')" -c ratings -- curl -sS productpage:9080/productpage | grep -o "<title>.*</title>"

#对外开放应用程序
kubectl apply -f $FILE

istioctl analyze

#部署入站IP和端口
#minikube tunnel#

export INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
export SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')

echo $INGRESS_HOST
echo $INGRESS_PORT
echo $SECURE_INGRESS_PORT


export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
echo "$GATEWAY_URL"
