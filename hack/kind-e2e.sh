#!/usr/bin/env bash
# Writable fixture installer; never linked into the guard9s binary.
set -euo pipefail
if [[ ${GUARD9S_KIND_E2E:-0} != 1 ]]; then
  echo 'Disabled. Set GUARD9S_KIND_E2E=1 to create an isolated disposable kind cluster.'
  exit 2
fi
for tool in kind docker kubectl go timeout; do command -v "$tool" >/dev/null || { echo "Missing tool: $tool"; exit 2; }; done
# Do not use remote/company Docker endpoints, inherited kubeconfig or contexts.
if [[ -n ${DOCKER_HOST:-} && ${DOCKER_HOST} != unix://* ]]; then echo 'Remote Docker endpoints are unsupported.'; exit 2; fi
docker_context=$(docker context show 2>/dev/null) || { echo 'Unable to resolve Docker context.'; exit 2; }
docker_endpoint=$(docker context inspect --format '{{(index .Endpoints "docker").Host}}' "$docker_context" 2>/dev/null) || { echo 'Unable to inspect Docker endpoint.'; exit 2; }
if [[ $docker_endpoint != unix://* ]]; then
  echo 'Disposable kind E2E requires a local Unix Docker endpoint.'
  exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd)
image=${GUARD9S_KIND_IMAGE:-kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0}
case "$image" in
  kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0|kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d) ;;
  *) echo 'Use a reviewed digest from the documented kind version matrix.'; exit 2 ;;
esac
umask 077
scratch=$(mktemp -d)
cluster="guard9s-e2e-${RANDOM}-$$"
admin="$scratch/admin.config"
artifacts=${GUARD9S_E2E_ARTIFACTS:-"$root/e2e-artifacts"}
mkdir -p "$artifacts"
created=0
cleanup() {
  result=$?
  trap - EXIT INT TERM
  if [[ $created == 1 ]]; then
    if [[ $result != 0 ]]; then
      # Only metadata/status summaries. Never dump Secrets, credentials or config.
      timeout 15s kubectl --kubeconfig "$admin" get nodes -o wide > "$artifacts/nodes.txt" 2>/dev/null || true
      timeout 15s kubectl --kubeconfig "$admin" get pods -A -o wide > "$artifacts/pods.txt" 2>/dev/null || true
    fi
    if timeout 90s kind delete cluster --name "$cluster" > /dev/null 2>&1; then
      if remaining=$(timeout 15s kind get clusters 2>/dev/null) && ! grep -Fx "$cluster" <<< "$remaining" >/dev/null; then
        echo 'Disposable kind cluster cleanup verified.'
      else
        echo 'Disposable kind cluster teardown could not be verified.'
        result=1
      fi
    else
      echo 'Disposable kind cluster teardown failed.'
      result=1
    fi
  fi
  rm -rf "$scratch"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Name is unique; refuse any accidental pre-existing cluster rather than reuse it.
if kind get clusters 2>/dev/null | grep -Fx "$cluster" >/dev/null; then echo 'Cluster name already exists.'; exit 2; fi
created=1
started=$SECONDS
export KUBECONFIG="$admin"
timeout 240s kind create cluster --name "$cluster" --image "$image" --config "$root/testdata/e2e/kind.yaml" --kubeconfig "$admin" --wait 180s
export GUARD9S_E2E_ADMIN_CONFIG="$admin" GUARD9S_E2E_CLUSTER="$cluster" GUARD9S_E2E_ARTIFACTS="$artifacts"
cd "$root"
timeout 600s go test -tags=e2e ./internal/e2e -count=1 -v -timeout=8m
printf 'kind integration duration: %ss\n' "$((SECONDS-started))"
