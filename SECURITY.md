# Security Policy

Please report suspected vulnerabilities privately through GitHub's **Report a
vulnerability** feature. Do not include credentials, exploit details, or other
sensitive data in a public issue.

Security fixes are supported on the latest release and the default branch.
Include reproduction steps, affected versions, and impact when possible.

## Deployment and scan data

Keep the populated `.env`, database password, session key, and provider API keys
out of source control and logs. `example.env` intentionally leaves credentials
empty. Preserve the session key with backups: it derives encryption keys as well
as session/CSRF keys. Configure HTTPS and keep `SECURE_COOKIES=true` for deployed
instances; disabling it is only for trusted local HTTP development.

The Compose examples publish database and Ollama ports. Restrict those ports to
trusted clients or loopback; do not expose an unauthenticated Ollama endpoint to
the public internet. Enable proxy trust flags only when that topology is trusted.

Successful device OCR sends text rather than the photo, but fallback can upload
the prepared image. Server-side OCR/LLM requests may send text, images, or approved
catalog references to the configured provider. Use trusted endpoints, keep keys
on the server, and review uncertain printing selections. Browser OCR workers and
language data are self-hosted; device OCR is not a guarantee of offline operation
or of photos never leaving the browser. See [CONFIGURATION.md](CONFIGURATION.md).
