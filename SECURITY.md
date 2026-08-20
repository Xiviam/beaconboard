# Security policy

BeaconBoard is a read-only monitor: its API cannot create or modify targets at runtime.

Please report vulnerabilities privately through GitHub's **Report a vulnerability**
feature instead of opening a public issue. Include a minimal reproduction, affected
version, and expected impact.

Configuration may contain authorization headers. BeaconBoard uses those headers only
for outbound probes and never exposes them through the dashboard, API, metrics, or logs.
Run the service behind an authenticated reverse proxy when its dashboard must not be
publicly visible.
