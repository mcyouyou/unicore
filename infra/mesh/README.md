

**mesh for discovering files**

service mesh

该部分使用istio进行服务发现、路由、流量管理生成
模拟进行测试

本地使用的版本:
istio-1.22.2

前置：
kubernetes,minikube(或者其他的集群，测试的时候使用的是minikube),docker

kubernetes版本:
使用kubectl version --client查看

Client Version: v1.30.2
Kustomize Version: v5.0.4-0.20230601165947-6ce0bf390ce3

docker:

在wsl(ubuntu)环境中测试,docker desktop 
engine version 27.0.3
containerd 1.7.18
runc 1.7.18
docker-init 0.19.0


testing.sh需要sudo权限

对于istio而言，访问网关会设置两个变量

minikube tunnel

该命令设置了一个minikube隧道，将流量发送到Istio Ingress Gateway
启用之后设置

