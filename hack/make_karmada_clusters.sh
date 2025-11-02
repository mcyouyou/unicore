
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512

kind create cluster --config kind-host.yaml -n host
kind create cluster --config kind-member1.yaml -n member1
kind create cluster --config kind-member2.yaml -n member2

echo "getting kubeconfigs.."

kind get kubeconfig --name host > host.kubeconfig
kind get kubeconfig --name member1 > member1.kubeconfig
kind get kubeconfig --name member2 > member2.kubeconfig

echo "init karmada for host.."

karmadactl init --kubeconfig=host.kubeconfig
alias kmc="kubectl --kubeconfig=/etc/karmada/karmada-apiserver.config"
alias kubectx="kubectl cluster-info --context"

echo "join member cluster to host.."

sed -i 's|server:.*:6444|server: https://154.19.43.15:6444|' host.kubeconfig
sed -i 's|server:.*:6445|server: https://154.19.43.15:6445|' member1.kubeconfig
sed -i 's|server:.*:6446|server: https://154.19.43.15:6446|' member2.kubeconfig

karmadactl join kind-member1 \
  --cluster-kubeconfig member1.kubeconfig \
  --kubeconfig=/etc/karmada/karmada-apiserver.config

karmadactl join kind-member2 \
  --cluster-kubeconfig member2.kubeconfig \
  --kubeconfig=/etc/karmada/karmada-apiserver.config
