# Deployment Guide

## Overview

This project ships with a Docker Compose stack that runs:
- the Go backend and finance dashboard
- PostgreSQL for invoice, payment, audit, and reconciliation data
- the OCR engine container for receipt processing

## Prerequisites

- Docker 20.10+
- Docker Compose 2.0+

## Start the stack

From the repository root:

```bash
docker compose up --build -d
```

This will:
1. build the backend image
2. build the OCR engine image
3. initialize PostgreSQL with the migration scripts
4. start the API on port 8080 and PostgreSQL on port 5432

## Verify the deployment

```bash
docker compose ps
curl http://localhost:8080/health
```

You should receive a JSON response similar to:

```json
{"status":"ok"}
```

## Access the application

- Finance dashboard: http://localhost:8080
- API health: http://localhost:8080/health
- PostgreSQL: localhost:5432
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000
- Alertmanager: http://localhost:9093

## Default login

The demo finance user is available in the backend for local deployment:
- username: finance
- password: finance123

## Stop the stack

```bash
docker compose down
```

To remove the database volume as well:

```bash
docker compose down -v
```

## Production hardening checklist

- replace the default JWT secret with a strong secret
- use managed PostgreSQL or a persistent volume in production
- restrict CORS origins rather than allowing all origins
- add TLS and a reverse proxy such as Nginx or Traefik
- store secrets in environment variables or a secret manager
