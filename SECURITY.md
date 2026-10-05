# Security policy

## v0.4 security boundary

- Demo runs offline without loading kubeconfig or invoking credential helpers.
- Live collection uses GET-only client-go informers. The HTTP transport rejects every non-GET request and also rejects GETs outside the explicit inventory resource allowlist, including sensitive/subresource paths such as Secrets, pod logs/status and workload scale.
- RBAC is limited to the explicitly listed inventory APIs and get/list/watch. Secrets, exec, logs, port-forward and eviction are not requested.
- Analyzer inputs contain data, never an API client. Plans are plain text; no shell or process launcher exists in the application.
- Exec/auth-provider credential plugins and insecure TLS are unsupported in V1. Kubeconfig is trusted local input; read-only credentials and a valid CA remain required.
- No remote telemetry, external AI analysis, uploads, automatic remediation or background agent is implemented.
- JSON/Markdown reports contain selected evidence, never raw manifests, container environment fields or kubeconfig paths/contents. Output is local stdout; no report upload is performed.
- Missing/stale evidence produces UNKNOWN. An interrupted core watch keeps an UNKNOWN collection finding; optional CSINode or VolumeAttachment failures invalidate only the affected CSI capability. Errors remain sticky until a new startup verifies the affected inventory.

Read-only does not mean nonsensitive: Pod specifications and Event messages can carry business information. The UI and exported reports avoid raw manifest/environment dumps, but resource names, labels-derived facts and event text can still be sensitive. Optional `--redact=basic` pseudonymizes known identifiers and detected addresses/images/paths while preserving risk and structured quantities. It is best-effort, not anonymization; arbitrary prose, encoded secrets and ambiguous identifiers may remain. Redacted commands are illustrative placeholders. Watch error events are sanitized before reflector logging/retries; expiration and RBAC revocation retain UNKNOWN. Keep reports and terminal recordings private and use only synthetic demo data in public examples. Never commit kubeconfig, tokens, secrets, certificates or company data.

## Supported versions

This is a pre-release v0.4 implementation. Disposable kind integration has validated the read-only boundary on Kubernetes v1.34.11 and v1.35.8, including zero observed collector non-GET requests. This is not a production safety guarantee or security audit; backend-specific CSI behavior and the operator's own test-cluster validation remain necessary before operational use.

## Reporting

Use the repository's private vulnerability reporting feature when available. Otherwise contact the maintainer through an existing private channel. Do not place credentials or exploitable sensitive details in a public issue. Include the affected commit, a synthetic reproduction and expected/actual behavior. No response-time SLA is currently promised.
