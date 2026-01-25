docker build --build-arg BUILD_TIME=$(date +%Y-%m-%dT%H:%M:%S) -t tksky1/unicore-scheduler-plugin:v1 .
#docker push tksky1/unicore-scheduler-plugin:v1
kind load docker-image tksky1/unicore-scheduler-plugin:v1