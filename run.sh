#install Istio
#using istio-1.22.2
#需要提前安装kubernetes以及minicube


#!/bin/bash

FILE="./istio-1.22.2"

if [ -e "$FILE" ]; then
    echo "$FILE exists"
else
    echo "$FILE does not exist,now installing istio"
    curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.22.2 TARGET_ARCH=x86_64 sh -
fi

export PATH="$PATH:./istio-1.22.2/bin"
istioctl install --set profile=demo -y
if [ -e "$FILE" ]; then
    istioctl install --set profile=demo -y
    kubectl label namespace default istio-injection=enabled
else
    echo "Failed to download istio"
fi
