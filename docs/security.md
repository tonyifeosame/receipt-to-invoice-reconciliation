# Security and Secret Management

## Secrets

- Store secrets in environment variables or a managed secret store such as Azure Key Vault, AWS Secrets Manager, or HashiCorp Vault.
- Do not commit secrets, API keys, or tokens to source control.

## Authentication and authorization

- Use strong JWT secrets and rotate them periodically.
- Consider enforcing MFA for privileged accounts.
- Use role-based access controls for finance staff and administrators.

## Application protections

- Add rate limiting for login, upload, and OCR endpoints.
- Implement account lockout after repeated failed login attempts.
- Enable CSRF protection for any browser-based state-changing requests that use cookies.
- Restrict file upload types and scan uploaded content for malware.

## Infrastructure

- Enforce TLS in transit.
- Restrict network access to only the required services.
- Apply principle-of-least-privilege permissions to service accounts.
