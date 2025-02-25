SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

source /root/go/pkg/mod/k8s.io/code-generator@v0.30.1/kube_codegen.sh

THIS_PKG="github.com/mcyouyou/unicore"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/api"

kube::codegen::gen_client \
    --with-watch \
    --output-dir "${SCRIPT_ROOT}/pkg/generated" \
    --output-pkg "${THIS_PKG}/pkg/generated" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/api"