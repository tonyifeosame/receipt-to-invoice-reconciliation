# Operations and Reliability Guide

## CI/CD

- GitHub Actions workflow is defined in [ci/github-actions.yml](../ci/github-actions.yml).
- Every push or pull request to the main branch runs the Go test suite and a backend build.

## Monitoring and observability

Recommended production stack:
- Prometheus for metrics collection
- Grafana for dashboards
- Loki or ELK for logs
- Alertmanager for alerts

Suggested metrics:
- HTTP request latency
- OCR processing duration
- reconciliation success rate
- payment volume per day
- database connection errors

## Backup and disaster recovery

- Schedule daily PostgreSQL backups using `pg_dump`.
- Store backup artifacts in object storage with retention policies.
- Test restore drills at least monthly.
- Keep application config and secrets versioned separately from runtime values.

## High availability and load balancing

- Run multiple backend replicas behind a load balancer.
- Use a managed PostgreSQL service or a primary-replica topology.
- Keep OCR workers horizontally scalable.
- Use health checks and automatic restarts for failed containers.

## Security hardening

- Replace the default JWT secret with a secret manager value.
- Enable rate limiting on authentication and upload endpoints.
- Add account lockout and password complexity checks.
- Restrict CORS to trusted origins.
- Enable TLS termination at the edge.
- Rotate credentials regularly and avoid storing secrets in code.

## Performance testing

Recommended approach:
- simulate 1,000+ receipt uploads with concurrent users
- measure OCR latency, API response times, and database throughput
- benchmark with a load testing tool such as k6 or Locust

## Documentation

- keep deployment, operations, and support docs up to date in [docs/deployment.md](deployment.md)
- include runbooks for common incidents and recovery steps
