#!/usr/bin/env bash
set -euo pipefail
guard9s_guard_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
bash -n "${guard9s_guard_root}/hack/live-test.sh"
bash "${guard9s_guard_root}/hack/live-test.sh" --help >/dev/null
if guard9s_guard_output="$(GUARD9S_LIVE_TEST=0 GUARD9S_LIVE_BINARY=/must-never-run bash "${guard9s_guard_root}/hack/live-test.sh" 2>&1)"; then
  printf '%s\n' 'Live-test default guard failed.' >&2
  exit 1
fi
if [[ "${guard9s_guard_output}" != *'Live-test disabled. Explicit GUARD9S_LIVE_TEST=1 is required.'* ]]; then
  printf '%s\n' 'Live-test did not stop before accessing configuration or binary.' >&2
  exit 1
fi
printf '%s\n' 'PASS: live-test syntax, help and disabled guard (no API access).'
