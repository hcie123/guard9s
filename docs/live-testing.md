# Read-only test-environment validation

Use an explicitly selected **personal or disposable test cluster**. Context
names alone do not prove an environment is safe; exclude company/production
contexts. guard9s performs inventory GETs with a resource allowlist. These steps
do not install RBAC, create fixtures or execute maintenance suggestions.

## 1. Choose a trusted binary

A prebuilt archive runs without Go. Follow the [installation guide](../README.md#installation)
and verify the matching archive checksum before extraction. A CI `SNAPSHOT` is
not a formal v0.4.0 release. Use `./guard9s` if the extracted binary is not in PATH.

```sh
guard9s --version
guard9s --help
guard9s --demo
```

Only source builds need Go 1.26+ and `make build`; their executable is
`./bin/guard9s`. The [manual v1.36.4 record](verification.md) exercised a prebuilt
Linux amd64 CI archive on a machine without Go.

## 2. Select the context and worker explicitly

First inspect local context names with `kubectl config get-contexts`. Set
`GUARD9S_TEST_CONTEXT` and `GUARD9S_TEST_NODE` to an explicitly approved test
context and Ready worker. Do not automatically use current-context or choose a
node. Missing or ambiguous inputs stop live validation.

The inventory permissions in [deploy/rbac.yaml](../deploy/rbac.yaml) are for
administrator review only. Core inventory requires get/list/watch; this guide
does not apply the manifest or change permissions. Denied optional CSINode or
VolumeAttachment access leaves affected evidence UNKNOWN.

## 3. Inspect the TUI

```sh
: "${GUARD9S_TEST_CONTEXT:?Set an explicitly approved test context}"
guard9s --context "$GUARD9S_TEST_CONTEXT"
```

Do not pass `--node` in TUI mode; choose the worker inside the interface. Check
READ ONLY, context, server version, node/pod counts and cache refresh, then inspect
Node, Pod, Risk, Events, Capacity, PDB, Storage, Diagnosis and Plan views. Plan
commands are text suggestions only; do not execute cordon/drain or any other
cluster mutation as part of validation.

## 4. Export and compare local reports

```sh
: "${GUARD9S_TEST_CONTEXT:?Set an explicitly approved test context}"
: "${GUARD9S_TEST_NODE:?Set an explicitly selected test worker}"
guard9s --context "$GUARD9S_TEST_CONTEXT" --node "$GUARD9S_TEST_NODE" \
  --output json > /tmp/guard9s-test.json
guard9s --context "$GUARD9S_TEST_CONTEXT" --node "$GUARD9S_TEST_NODE" \
  --output json --redact=basic > /tmp/guard9s-test-redacted.json
python3 -m json.tool /tmp/guard9s-test.json >/dev/null
python3 -m json.tool /tmp/guard9s-test-redacted.json >/dev/null
```

Confirm schema `guard9s/v1`, readOnly, readiness, findings, capabilities and
scheduling candidates. Compare severity/UNKNOWN, structured capacity quantities
and candidate status as well as counts. Two live invocations can observe real
inventory changes; matching counts alone is not a proof of identical evidence.
Unit regressions compare redaction of the same snapshot separately.

PDB or storage resources that are absent should be recorded as absent; do not
create them to improve coverage. Zero VolumeAttachment objects does not validate
backend attachment behavior. A third-party controller such as StrimziPodSet can
leave owner evidence UNKNOWN. Capacity is based on requests and scheduling
evidence, not live CPU utilization.

Keep both reports on the test machine. Basic redaction is pseudonymization,
not complete anonymization; event messages and arbitrary business prose can
remain sensitive. Do not commit reports, upload them as artifacts or attach them
to public issues. Allow local review before removing temporary reports.

## 5. Record only sanitized results

Record the date, server version, binary version/commit/checksum when available,
test-environment class, anonymized object counts, collection/capability results,
readiness and sanitized errors. Use generic worker/context labels. Do not record
real node/namespace/pod/context names, internal IPs, machine paths, credentials or
kubeconfig contents. Do not claim server-side zero writes without API audit-log
evidence; transport tests and the CI reader audit have their own stated scope.

If a correctness or security problem appears, stop, retain a local sanitized
reproduction and report expected versus actual behavior. Do not mutate the
cluster or add a workaround during release validation.

## Optional harness from a source checkout

The existing harness remains default-off and is not required to run a prebuilt
binary. From a source checkout, a trusted binary may be supplied explicitly:

```sh
GUARD9S_LIVE_TEST=1 \
GUARD9S_LIVE_KUBECONFIG=/absolute/path/to/test-kubeconfig \
GUARD9S_LIVE_CONTEXT=test-context \
GUARD9S_LIVE_NODE=test-worker-1 \
GUARD9S_LIVE_BINARY=/absolute/path/to/guard9s \
bash hack/live-test.sh
```

Replace all example values with approved local inputs. It runs one protected
collection and prints basic-redacted JSON; it does not create, apply, patch,
delete, exec or port-forward anything. The v1.36.4 manual record used direct
binary invocations and does not claim this harness was run.

## Separate CI evidence

CI uses disposable three-node kind Kubernetes v1.34.11/v1.35.8 clusters. The
writable fixture-admin client is excluded from the production binary; guard9s
uses a separate read-only identity. That integration evidence does not establish
general production or CSI-backend compatibility.
