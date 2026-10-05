# Contributing to guard9s

Start with `make demo` and read the analysis contract. This project lives in a shared repository: modify only `guard9s/` and its dedicated CI workflow. Work on a branch and open a pull request; do not push changes directly to main.

## Development

Requires Go 1.26+, Make (optional) and a terminal. `make check` runs format checks, vet, unit tests, the race detector, a 90% aggregate core coverage gate and a static binary build. `make snapshot` regenerates the actual demo screen used by the README. GoReleaser snapshot packaging is optional; this configuration does not publish releases.

## Changes we welcome

A focused bug report or PR should explain the user-visible problem, evidence, expected behavior, limitations and meaningful verification. Analyzer regressions should have synthetic fixtures demonstrating both safe and unsafe cases. Keep UI and collection separate from analysis. New analyzers implement the Analyzer interface and must return UNKNOWN when evidence is insufficient.

Use fake.NewSimpleClientset for unit API tests. Ordinary tests never require a real kubeconfig, cloud credential, company endpoint or live cluster. The separately gated E2E harness creates its own disposable kind cluster and synthetic credentials, then verifies teardown. Do not include real node/namespace/workload names, internal prompts, customer data, tokens, certificates or configs in fixtures, screenshots or logs.

V1 has no cluster mutation path. Do not add shell execution, kubectl wrappers, exec, logs, port-forward, patch/apply/delete, eviction or write permissions. Suggested commands must remain text, use the selected context and preserve drain safeguards. Changes to the security contract need explicit maintainer discussion.

## Before submitting

- Run `make check`; inspect the complete diff and new files.
- Verify that blocked/unknown evidence cannot produce a READY plan.
- Review request accounting and namespace scope if changing capacity.
- Update documentation for behavior changes and known limits.
- Ensure only project-specific files and its workflow changed.

Contributor conduct is defined in CODE_OF_CONDUCT.md. Security reports follow SECURITY.md. Contributions are accepted under this project's Apache-2.0 license.
