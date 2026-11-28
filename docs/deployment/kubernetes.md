---
title: Kubernetes
description: Deploy opnsense2otel on Kubernetes with secrets, Deployment manifests, and Prometheus Operator scrape configuration
tags:
  - Deployment
  - Kubernetes
---

# Kubernetes Deployment

Deploy opnsense2otel in a Kubernetes cluster with file-based secrets and Prometheus integration.

## Prerequisites

- A Kubernetes cluster with kubectl access
- OPNsense API credentials (key and secret)
- Optional: [Prometheus Operator](https://prometheus-operator.dev/) for automated scrape configuration

## Helm chart

The signed Helm chart is published as an OCI package. Create the API credential Secret in the
release namespace before installing the chart. Its default keys are `key` and `secret`; set
`opnsense.existingSecret` to its name. For other key names, set `opnsense.secretKeys.apiKey` and
`opnsense.secretKeys.apiSecret`. After rotating the Secret, change `opnsense.secretRevision` to a
non-secret revision string and upgrade to roll the pod.

Choose a published release and replace `3.0.0` below with its version without the leading `v`:

```sh
helm upgrade --install edge-fw oci://ghcr.io/rknightion/charts/opnsense2otel \
  --version 3.0.0 --namespace monitoring \
  --set opnsense.address=edge-fw.example.net \
  --set opnsense.existingSecret=edge-fw-api
```

Syslog, Zenarmor and NetFlow receivers are disabled by default. Enabling a receiver under
`receivers.*` adds its flags and ports; set its `allowedPeers` CIDR list to restrict senders.
Prefer the schema-checked `settings` map for non-secret exporter flags: its keys are flag names
without the leading dashes. Reserved keys fail the render, including entire receiver flag
prefixes. Use a dedicated chart value where one exists; some reserved flags have no chart value
and cannot be configured through the chart. See the [chart README](https://github.com/rknightion/opnsense2otel/tree/main/charts/opnsense2otel#readme)
for values and receiver examples.

Both `settings` and `extraArgs` become container arguments; do not put tokens, passwords or header
values in either. The chart mounts only the API key pair. Other secrets have no chart mount route
yet; see [issue #1124 (mounting other chart secrets)](https://github.com/rknightion/opnsense2otel/issues/1124).

Steps 1 to 3 below describe the separate raw-manifest route, not the Helm installation.

## Step 1: Create the Secret

When you [generate API keys](https://docs.opnsense.org/development/how-tos/api.html#creating-keys) on OPNsense, you get a `.txt` file with the key and secret. Add your OPNsense host and protocol to this file:

```text title="opnsense_apikey.txt"
key=xt...Nt
secret=EK...ho
host=opnsense.lan
protocol=https
```

Create the Secret in your cluster:

```bash
kubectl create secret generic opnsense2otel-cfg \
  --from-env-file=opnsense_apikey.txt
```

## Step 2: Deploy the exporter

The following manifest creates a Deployment and a ClusterIP Service. API credentials are mounted as files from the Secret, and connection settings are injected as environment variables.

<!-- docgen:begin:kubernetes-deployment -->
```yaml title="deployment.yaml"
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: opnsense2otel
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: opnsense2otel
  template:
    metadata:
      labels:
        app.kubernetes.io/name: opnsense2otel
    spec:
      # The exporter is a pure HTTP scrape target and never calls the Kubernetes API,
      # so don't mount a ServiceAccount token (shrinks the pivot surface on compromise).
      automountServiceAccountToken: false
      # Validate the same environment and mounted credentials before replacing a
      # working pod. --config.check performs no API call and binds no listener.
      initContainers:
        - name: config-check
          image: ghcr.io/rknightion/opnsense2otel:5.4.1 # x-release-please-version
          imagePullPolicy: IfNotPresent
          args:
            - "--log.level=info"
            - "--log.format=json"
            - "--config.check"
          env:
            - name: OPN2OTEL_INSTANCE_LABEL
              value: "opnsense"
            - name: OPN2OTEL_OPS_API
              valueFrom:
                secretKeyRef:
                  name: opnsense2otel-cfg
                  key: host
            - name: OPN2OTEL_OPS_PROTOCOL
              valueFrom:
                secretKeyRef:
                  name: opnsense2otel-cfg
                  key: protocol
            - name: OPS_API_KEY_FILE
              value: /etc/opnsense2otel/creds/api-key
            - name: OPS_API_SECRET_FILE
              value: /etc/opnsense2otel/creds/api-secret
          volumeMounts:
            - name: api-key-vol
              mountPath: /etc/opnsense2otel/creds
              readOnly: true
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            seccompProfile:
              type: RuntimeDefault
      containers:
        - name: opnsense2otel
          # Pin to an immutable release tag, not :latest — :latest is retagged every
          # release, so a pod reschedule could silently pull a breaking version with no
          # deploy event to correlate. The tag tracks version.txt and is rewritten on
          # each release by release-please via the x-release-please-version marker below;
          # keep that marker on the image line or the tag stops updating. Published tags
          # carry no leading "v" (the git tag is v2.2.1, the image tag is 2.2.1).
          image: ghcr.io/rknightion/opnsense2otel:5.4.1 # x-release-please-version
          imagePullPolicy: IfNotPresent
          # In pod, mount OPNSense API credentials as files
          volumeMounts:
            # name must match the volume name below
            - name: api-key-vol
              mountPath: /etc/opnsense2otel/creds
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            seccompProfile:
              type: RuntimeDefault
          ports:
            # Default value for --web.listen-address= is 8080
            - name: metrics-http
              containerPort: 8080
            # The syslog receiver (--logs.syslog.enabled), off by default. Both
            # protocols are declared because OPNsense can be configured to send over
            # either; the exporter listens on both by default. Port 5514, not 514:
            # 514 is privileged and this container runs as a non-root user.
            - name: syslog-udp
              containerPort: 5514
              protocol: UDP
            - name: syslog-tcp
              containerPort: 5514
              protocol: TCP
            # The Zenarmor receiver (--logs.zenarmor.enabled), off by default.
            - name: zenarmor-tcp
              containerPort: 9200
              protocol: TCP
            # The NetFlow receiver (--flow.netflow.enabled), off by default.
            - name: netflow-udp
              containerPort: 2055
              protocol: UDP
          # /-/healthy: process liveness only — no firewall dependency. Do NOT
          # point readinessProbe at /-/ready here: when Prometheus scrapes via
          # Service endpoints, a not-ready pod stops being scraped and the
          # opnsense_up=0 signal is lost exactly when the firewall is down.
          livenessProbe:
            httpGet:
              path: /-/healthy
              port: metrics-http
          readinessProbe:
            httpGet:
              path: /-/healthy
              port: metrics-http
          # See main readme; some configuration options can be set via env-vars
          ##
          args:
            - "--log.level=info"
            - "--log.format=json"
          env:
            - name: GOMEMLIMIT
              valueFrom:
                resourceFieldRef:
                  resource: limits.memory
            - name: OPN2OTEL_INSTANCE_LABEL
              value: "opnsense"
            - name: OPN2OTEL_OPS_API
              valueFrom:
                secretKeyRef:
                  name: opnsense2otel-cfg
                  key: host
            - name: OPN2OTEL_OPS_PROTOCOL
              valueFrom:
                secretKeyRef:
                  name: opnsense2otel-cfg
                  key: protocol
            # Env var points to a location on disk, make sure the value set here matches the volumeMount path
            - name: OPS_API_KEY_FILE
              value: /etc/opnsense2otel/creds/api-key
            - name: OPS_API_SECRET_FILE
              value: /etc/opnsense2otel/creds/api-secret
            # Only enable if using self-signed cert
            # - name: OPN2OTEL_OPS_INSECURE
            #   value: "true"

          # in basic testing with a home lab OPNsense 100m CPU and 64Mi memory are sufficient
          # however if your opnsense instance has a large number of rules, interfaces, etc...
          # you may need to adjust these values
          resources:
            requests:
              memory: 64Mi
              cpu: 100m
            limits:
              memory: 128Mi
              cpu: 500m
      volumes:
        - name: api-key-vol
          secret:
            secretName: opnsense2otel-cfg
            items:
              - key: key
                path: api-key
              - key: secret
                path: api-secret
---
kind: Service
apiVersion: v1
metadata:
  name: opnsense2otel
spec:
  selector:
    app.kubernetes.io/name: opnsense2otel
  type: ClusterIP
  ports:
    - name: http
      protocol: TCP
      port: 8080
      targetPort: 8080
    # Syslog receiver (--logs.syslog.enabled), off by default. A ClusterIP is only
    # reachable from inside the cluster, so the firewall cannot push to it as-is:
    # to use the receiver, expose these via a LoadBalancer/NodePort Service (or set
    # `type: LoadBalancer` here) and point the OPNsense logging target at that
    # address. See docs/syslog-receiver.md.
    - name: syslog-udp
      protocol: UDP
      port: 5514
      targetPort: 5514
    - name: syslog-tcp
      protocol: TCP
      port: 5514
      targetPort: 5514
    # Zenarmor's Elasticsearch-shaped receiver, off by default. As with syslog,
    # expose this ClusterIP through a LoadBalancer/NodePort before pointing the
    # firewall at it.
    - name: zenarmor-tcp
      protocol: TCP
      port: 9200
      targetPort: zenarmor-tcp
    # NetFlow receiver, off by default and unauthenticated. Restrict the external
    # exposure to the firewall when converting this Service type.
    - name: netflow-udp
      protocol: UDP
      port: 2055
      targetPort: netflow-udp
```
<!-- docgen:end:kubernetes-deployment -->

Apply the manifest:

```bash
kubectl apply -f deployment.yaml
```

## Memory sizing

The chart and manifest set the main container's `GOMEMLIMIT` from its own
`limits.memory` using the downward API. Changing that limit changes the value on
pod creation. If no container memory limit is set, Kubernetes can supply node
allocatable memory instead; set a limit for a container-specific target. The init
container is unchanged.
`GOMEMLIMIT` is a soft Go runtime target, not a hard container memory cap: it
excludes some memory, and the runtime may exceed it to make progress. Kubernetes
can still terminate the container for exceeding its limit. Leave headroom and
measure peak container memory under representative traffic, including sink outages.

Size for the enabled workload, not just the example limit. These are component
estimates and configured ceilings, **not measured peak container memory**; actual
usage depends on traffic, payloads and the firewall inventory.

| Component | Sizing guidance |
| --- | --- |
| Baseline polling | The example requests 64 MiB and limits memory to 128 MiB, not a measured requirement. Account for the runtime, API responses and collector snapshots; GeoIP Country and ASN editions add roughly 9 MB and 12 MB respectively when loaded. |
| Syslog queue case | `--logs.buffer-size` defaults to 65,536 records and `--logs.buffer-max-bytes` to 128 MiB. At an estimated 1,356 B per example syslog record, the count cap is about 89 MB. |
| Zenarmor queue case | The same queue, at an estimated 4,034 B per example Zenarmor record, reaches its 128 MiB byte budget at about 33,000 records, before the count cap. |
| Zenarmor concurrent ingest | Each request is limited to 64 MiB; `--logs.zenarmor.max-concurrent-requests` defaults to 8, so bodies alone can total 512 MiB, before decoding and response overhead. |
| Flow correlator | `--flow.correlate.max-entries` defaults to 50,000; at roughly 2.2 to 3.2 KB per entry, allow about 110 to 160 MB at capacity when correlation is active. |
| Flow rollup | `--flow.max-keys` defaults to 100,000, roughly 25 to 35 MB at capacity. |

The queue rows are alternative record-shape examples for **one shared queue**,
not additive budgets; mixed traffic competes for the same count and byte limits.
See [log shipping](../log-shipping.md) and [flow metrics](../flow.md) for the
bounds and monitoring signals.

Raise the container limit or reduce the relevant bounds when measurements show
insufficient headroom; neither the soft target nor queue limits guarantee that a
container cannot be OOM-killed.

## Step 3: Configure Prometheus scraping

### Prometheus Operator (ScrapeConfig)

If you are running the Prometheus Operator, create a `ScrapeConfig` resource:

```yaml title="scrape.yaml"
apiVersion: monitoring.coreos.com/v1alpha1
kind: ScrapeConfig
metadata:
  name: opnsense2otel
  labels:
    # Match the label selector your Prometheus uses for ScrapeConfig discovery
    release: "kube-prom"
spec:
  scrapeInterval: 60s
  # This bounds only the HTTP snapshot replay. OPNsense API polling runs in the
  # background under its own poll intervals and request timeouts, so scrapeTimeout
  # no longer controls collector fan-out.
  scrapeTimeout: 30s
  metricsPath: /metrics
  staticConfigs:
    - labels:
        job: opnsense2otel
      targets:
        - opnsense2otel.default.svc:8080
```

### Prometheus Operator (ServiceMonitor)

Alternatively, use a `ServiceMonitor`:

```yaml title="servicemonitor.yaml"
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: opnsense2otel
  labels:
    release: "kube-prom"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: opnsense2otel
  endpoints:
    - port: http
      interval: 30s
      path: /metrics
```

### Static Prometheus config

If you are not using the Prometheus Operator, add a scrape job to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: opnsense
    scrape_interval: 30s
    static_configs:
      - targets:
          - opnsense2otel.default.svc:8080
```

## Verify the deployment

```bash
kubectl run debug --rm -i --tty --restart=Never --image=alpine - \
  wget --quiet -O- opnsense2otel.default.svc.cluster.local:8080/metrics | head -20
```

## Security considerations

The deployment manifest is hardened:

- **Read-only root filesystem** - no writable paths in the container
- **Non-root user** - runs as UID 65532
- **Dropped capabilities** - all Linux capabilities are dropped
- **No privilege escalation** - `allowPrivilegeEscalation: false`
- **File-based secrets** - API credentials are mounted as files, not passed as environment variables

!!! tip "Self-signed certificates"
    If your OPNsense uses a self-signed certificate, add `OPN2OTEL_OPS_INSECURE: "true"` to the env section. For production, add the CA certificate to the container's trust store instead.

## Restrict access with a NetworkPolicy

The `/metrics` endpoint requires no authentication by default, so restrict which pods can reach it. A sample manifest is provided at [`deploy/k8s/networkpolicy.yaml`](https://github.com/rknightion/opnsense2otel/blob/main/deploy/k8s/networkpolicy.yaml):

```yaml title="networkpolicy.yaml"
kind: NetworkPolicy
apiVersion: networking.k8s.io/v1
metadata:
  name: opnsense2otel
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: opnsense2otel
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              # Placeholder: the namespace your monitoring stack runs in
              kubernetes.io/metadata.name: monitoring
          podSelector:
            matchLabels:
              # Placeholder: the labels on your Prometheus pods
              app.kubernetes.io/name: prometheus
      ports:
        - protocol: TCP
          port: 8080
```

Adjust the `namespaceSelector` and `podSelector` placeholders to match where your Prometheus runs, then apply:

```bash
kubectl apply -f networkpolicy.yaml
```

!!! note
    NetworkPolicy is only enforced if your cluster's CNI supports it (Calico, Cilium, and similar). On clusters without a policy-capable CNI the manifest is accepted but has no effect.

## Disabling collectors

Add disable flags to the `args` array in the Deployment:

```yaml
args:
  - "--log.level=info"
  - "--log.format=json"
  - "--exporter.disable-cron-table"
  - "--exporter.disable-arp-table"
```

Or use environment variables:

```yaml
env:
  - name: OPN2OTEL_DISABLE_CRON_TABLE
    value: "true"
```
