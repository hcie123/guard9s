#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<'HELP'
guard9s read-only live-test (disabled by default)

Required environment:
  GUARD9S_LIVE_TEST=1
  GUARD9S_LIVE_KUBECONFIG=/absolute/path/to/personal-test-config
  GUARD9S_LIVE_CONTEXT=personal-test-context
  GUARD9S_LIVE_NODE=personal-test-node

Build with make build first. Uses the compiled guard9s GET-only collector and
exports a basic-redacted JSON report to stdout. It never creates workloads,
applies/patches/deletes resources, executes pods, or opens a port-forward.
Do not select a company or production context. This script cannot determine
whether an operator-selected endpoint is production; selection is explicit.

Optional: GUARD9S_LIVE_BINARY=/absolute/path/to/a/trusted/guard9s-binary
HELP
  exit 0
fi
if [[ $# -ne 0 ]]; then
  printf '%s\n' 'Unexpected argument; use --help.' >&2
  exit 2
fi

# This check precedes binary, kubeconfig, context and endpoint access.
if [[ "${GUARD9S_LIVE_TEST:-0}" != "1" ]]; then
  printf '%s\n' 'Live-test disabled. Explicit GUARD9S_LIVE_TEST=1 is required.' >&2
  exit 2
fi
if [[ -z "${GUARD9S_LIVE_KUBECONFIG:-}" || -z "${GUARD9S_LIVE_CONTEXT:-}" || -z "${GUARD9S_LIVE_NODE:-}" ]]; then
  printf '%s\n' 'Explicit personal-test kubeconfig, context and node are required; see --help.' >&2
  exit 2
fi
if [[ "${GUARD9S_LIVE_KUBECONFIG}" != /* || ! -f "${GUARD9S_LIVE_KUBECONFIG}" ]]; then
  printf '%s\n' 'GUARD9S_LIVE_KUBECONFIG must name an existing absolute file path.' >&2
  exit 2
fi

guard9s_live_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
guard9s_live_binary="${GUARD9S_LIVE_BINARY:-${guard9s_live_root}/bin/guard9s}"
if [[ ! -x "${guard9s_live_binary}" ]]; then
  printf '%s\n' 'A trusted guard9s binary is required; run make build first.' >&2
  exit 2
fi

exec "${guard9s_live_binary}" \
  --kubeconfig "${GUARD9S_LIVE_KUBECONFIG}" \
  --context "${GUARD9S_LIVE_CONTEXT}" \
  --node "${GUARD9S_LIVE_NODE}" \
  --timeout 30s --output json --redact=basic
