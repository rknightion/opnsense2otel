# Live API contracts

`just live-contract` runs the Go `live`-tag contract suite with a 20-minute total
timeout. It uses the same read-only endpoint schemas, request bodies, parameter
resolvers and profile exemptions as the schema canary. The suite fails on failed
transport, authentication, invalid JSON, incompatible consumed-field types, and
missing core endpoints. Only known plugin endpoints may skip on HTTP 404;
parameter-dependent requests skip when their resource is absent. Additive fields
and box-state warnings remain the schema canary's responsibility.

The existing on-demand live-canary workflow runs this recipe once per profile
inside its own lab power lifecycle. Do not run it concurrently with another
consumer of the shared testbed. No new schedule or credentials are introduced.

Local invocation requires these existing environment inputs:

- `DEVBOX_HOST`: HTTPS host, optionally with a port; no scheme or path.
- `OPN2OTEL_OPS_API_KEY` and `OPN2OTEL_OPS_API_SECRET`: API credentials.
- `CANARY_PROFILE`: `nightly` or `release-vm`.
- `OPN2OTEL_LIVE_REQUIRED`: set in CI so missing configuration fails.

With every input absent locally, the live suite explicitly skips. Partial or
invalid configuration fails. The testbed credentials remain in the existing
GitHub environment; local invocation is not a reason to copy them to a laptop.
The client accepts the existing lab's self-signed certificates, negotiates HTTP/2,
and never follows redirects. Core preflight requests have a 10-second timeout;
subsequent requests use the canary's 30-second timeout, including slow firmware
responses. The existing single transport retry remains bounded by the suite limit.

Diagnostics report endpoint names and fixed failure classes. Raw transport errors,
response bodies, hosts and credentials are not printed or captured.

The credential-free HTTP fixture tests run in ordinary `just test` and therefore
in `just check`. To run only those checks:

```sh
just test 'TestLive(ContractVerdicts|Configuration)$'
```

They prove clean and broken shapes, core versus plugin absence, authentication,
invalid JSON, and missing versus required configuration. They do not substitute
for a successful live run against both profiles.
