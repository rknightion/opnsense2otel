# opnsense2otel

This Helm v3 application chart deploys one secure OPNsense exporter for one firewall. Install a separate Helm release for every additional firewall; the Deployment is deliberately fixed at one replica.

## Install

Create the API credential Secret outside Helm. By default its keys must be the canonical raw-manifest names `key` and `secret`; the chart mounts them as the internal files `api-key` and `api-secret` and never accepts or renders credential values.

Install the signed OCI package, pinning a published release version without the leading `v`.
Replace `3.0.0` with the release you choose and create the namespace and Secret first:

```sh
helm upgrade --install edge-fw oci://ghcr.io/rknightion/charts/opnsense2otel \
  --version 3.0.0 --namespace monitoring \
  --set opnsense.address=edge-fw.example.net \
  --set opnsense.existingSecret=edge-fw-api
```

For a source checkout instead:

```sh
kubectl -n monitoring create secret generic edge-fw-api \
  --from-literal=key='...' \
  --from-literal=secret='...'

helm upgrade --install edge-fw charts/opnsense2otel \
  --namespace monitoring --create-namespace \
  --set opnsense.address=edge-fw.example.net \
  --set opnsense.existingSecret=edge-fw-api
```

`opnsense.address` is required when the chart renders. Use a hostname or IP address without a scheme; set `opnsense.protocol` to `http` only when that is genuinely required. Set `opnsense.secretKeys.apiKey` and `opnsense.secretKeys.apiSecret` only when an existing Secret uses names other than `key` and `secret`.

After rotating an existing Secret, set `opnsense.secretRevision` to a non-secret revision string and upgrade the release. The chart adds that string to the pod template so Kubernetes rolls the exporter without reading or hashing Secret contents.

## Versions and release packages

The source chart tracks the development image with `appVersion: main`, so an empty `image.tag` renders `ghcr.io/rknightion/opnsense2otel:main`. The release publisher overrides `appVersion` with the release version when building packages. It signs and pushes each package to `oci://ghcr.io/rknightion/charts/opnsense2otel` and attaches `opnsense2otel-<version>.tgz` to the matching GitHub release.

## Optional receivers

All push receivers are disabled by default, so their Service and container ports are absent until enabled. This chart exposes the fixed non-privileged receiver ports: syslog UDP and TCP `5514`, Zenarmor TCP `9200`, and NetFlow UDP `2055`.

```yaml
receivers:
  syslog:
    enabled: true
    allowedPeers: ["10.0.0.254/32"]
  zenarmor:
    enabled: true
    allowedPeers: ["10.0.0.254/32"]
  netflow:
    enabled: true
    allowedPeers: ["10.0.0.254/32"]
```

The chart adds the corresponding exporter flags: `--logs.enabled --logs.syslog.enabled`, `--logs.enabled --logs.zenarmor.enabled`, and `--flow.enabled --flow.netflow.enabled`. Peer allowlists become receiver arguments, not Kubernetes labels.

For non-secret exporter flags, prefer the generated, schema-checked `settings` map over the free-text `extraArgs` array. Keys are flag names without the leading dashes. Reserved flags fail rendering in `settings` or `extraArgs`, including entire receiver flag prefixes. Use a dedicated chart value where one exists; some reserved flags have no chart value and cannot be configured through the chart. The settings inventory is in `values.yaml`.

Syslog and Zenarmor feed the log-shipping pipeline. Its default sink is OTLP, so configure non-secret `--otlp.*` transport settings through `settings`, or select `--logs.sink=stdout`. The chart does not silently replace the exporter's sink default.

Both `settings` and `extraArgs` become container arguments. Use `extraEnv` to supply tokens, passwords and header values from an existing Kubernetes Secret to both the configuration check and exporter containers:

```yaml
extraEnv:
  - name: OPN2OTEL_OTLP_HEADERS
    valueFrom:
      secretKeyRef:
        name: exporter-otlp
        key: headers
```

The API key and secret come from mounted files in the Secret selected by `opnsense.existingSecret`. Those files take precedence over their environment variables, so update that Secret to rotate the API pair. The generated settings inventory names the environment variable for each credential flag. The chart continues to accept legacy credential flags, but Helm release records and container arguments expose their values.

The pod runs as UID/GID `65532`, has no service-account token, drops all capabilities, disallows privilege escalation, uses a read-only root filesystem, and sets the RuntimeDefault seccomp profile. A hardened `config-check` init container validates configuration and mounted credentials before the exporter starts.

## Grafana assets

This chart does not package dashboards, alert rules, or Prometheus Operator CRDs. Manage Grafana dashboards and rules through the repository's supported Grafana artifacts.

## Validation

Run the chart-local render contracts:

```sh
bash charts/opnsense2otel/tests/test-chart.sh
```

They cover Helm linting, minimal and optional-receiver renders, schema failures, credential references, pod hardening, upgrade rendering, and kubeconform when it is installed.
