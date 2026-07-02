# Backup and Disaster Recovery

## Backup strategy

1. Take daily PostgreSQL logical backups with `pg_dump`.
2. Store backups in durable object storage with encryption at rest.
3. Retain multiple backup generations and verify restoreability.

## Recovery procedure

1. Provision a fresh PostgreSQL instance.
2. Restore the latest known-good backup.
3. Rebuild application containers and redeploy.
4. Validate dashboard access, data integrity, and OCR functionality.

## Operational checklist

- confirm backup schedule
- verify backup retention policy
- test a restore drill monthly
- document rollback steps for each release
