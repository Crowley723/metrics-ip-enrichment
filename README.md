# metrics-ip-enrichment

Enriches Prometheus metrics that contain IP addresses with geo and ASN data from MaxMind GeoLite2 databases. The additional context can be used for per-country dashboards, traffic spike alerting by origin, and quickly determining whether anomalous traffic is coming from a single ASN or a distributed set of addresses.

## How it works

On each interval, the service:
1. Queries Mimir for a configured PromQL metric
2. Extracts the IP from a configured label on each time series
3. Looks up the IP in the GeoLite2 City and ASN databases
4. Remote_writes the enriched metric back to Mimir with added labels: `country_code`, `country_name`, `asn`, `asn_org`

Multiple jobs can run in the same process. Each job has its own source query, output metric name, IP label, interval, and enricher list.

## Requirements

- MaxMind GeoLite2 City and ASN `.mmdb` files (not included)
- A running Mimir instance with remote_write enabled
- Go 1.26.2+ to build from source, or Docker

## Configuration

```yaml
mimir:
  query_url: https://mimir.example.com/prometheus
  push_url:  https://mimir.example.com/api/v1/push
  org_id:    anonymous
  username:  your-user
  password:  your-password

mmdb:
  city: /path/to/GeoLite2-City.mmdb
  asn:  /path/to/GeoLite2-ASN.mmdb

jobs:
  - name: traefik_by_client_ip
    source_query: traefik:requests:by_client_ip
    output_metric: traefik:requests:by_client_ip:enriched
    ip_label: ClientIP
    interval: 60s
    enrichers: [geoip_city, geoip_asn]
```

Available enrichers: `geoip_city`, `geoip_asn`

Config is validated at startup. The process exits immediately if any field is missing or invalid, rather than failing at runtime.

## Running

```bash
go run main.go -c config.yaml
```

Or build first:

```bash
go build -o metrics-ip-enrichment .
./metrics-ip-enrichment -c config.yaml
```

## Docker

Build:

```bash
docker build -t metrics-ip-enrichment .
```

Run with local MMDBs and a local config file:

```bash
docker run --rm \
  -v "$(pwd)/GeoLite2-City.mmdb:/etc/metrics-ip-enrichment/GeoLite2-City.mmdb:ro" \
  -v "$(pwd)/GeoLite2-ASN.mmdb:/etc/metrics-ip-enrichment/GeoLite2-ASN.mmdb:ro" \
  -v "$(pwd)/config.local.yaml:/etc/metrics-ip-enrichment/config.yaml:ro" \
  -p 8080:8080 \
  metrics-ip-enrichment
```

Your config's `mmdb` paths must match the in-container mount paths shown above.

## Kubernetes

The container expects:

- `config.yaml` mounted at `/etc/metrics-ip-enrichment/config.yaml` (use a `Secret`)
- MMDB files mounted at the paths specified in your config (use a `PersistentVolumeClaim`, populated by an init container that downloads from MaxMind using a license key)

Health endpoint: `GET /healthz` on port `8080`. Wire this up as your liveness and readiness probe.

Pre-built images are pushed to `ghcr.io/crowley723/metrics-enrichment` on every merge to `main`, tagged `sha-<commit>` and `latest`.

## Adding an enricher

Implement the `enrich.Enricher` interface and register a constructor in `enrich.DefaultRegistry`. The job runner picks it up by name from the config's `enrichers` list.
