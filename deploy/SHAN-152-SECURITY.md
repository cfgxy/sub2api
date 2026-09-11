# SHAN-152 Secret Contract

The enterprise identity module does not define a second token secret. At startup, `LoadForBootstrap` reads the deployment's `jwt.secret`; after database migrations, repository bootstrap creates `security_secrets.jwt_secret` only when it is absent. If a row already exists, the persisted value wins over the configured value for cross-instance consistency. Generic bootstrap may generate and persist a missing JWT secret, but this SHAN-152 compose entrypoint requires `SHAN152_JWT_SECRET` before the app starts. The resolved value is validated again after bootstrap, and startup fails closed before token handlers are available when it is shorter than 32 bytes.

For the isolated SHAN-152 stack, the secret source is the deployment secret provider or the local operator environment:

- `SHAN152_JWT_SECRET` is required and mapped to `JWT_SECRET` only in the app process.
- `SHAN152_TOTP_ENCRYPTION_KEY` is required and mapped to `TOTP_ENCRYPTION_KEY`; it supplies the AES-256-GCM key used to encrypt a separately configured object store's `SecretAccessKey` before the settings JSON is persisted.
- PostgreSQL data, Redis data, network names, and containers use SHAN-152-specific names, so another environment cannot supply the runtime secret or token state accidentally.

Do not put either value in tracked YAML, compose files, command arguments, logs, or issue comments. Production orchestration must inject both values from its managed secret store. Local development may export them in the invoking shell. The compose file contains required-variable references only and aborts before startup when either value is absent.

Brand backgrounds use the configured image object store. For a separately configured image store, the settings service refuses a new `SecretAccessKey` unless the encryption key was explicitly configured, encrypts only that secret field before persistence, and decrypts it only while constructing the runtime storage client. Endpoint, region, bucket, prefix, path-style mode, and `AccessKeyID` are configuration metadata rather than encrypted fields. When image storage reuses backup S3, it does not persist another credential copy and instead consumes the backup service's resolved credentials. Settings responses clear `SecretAccessKey`, and the enterprise brand service receives only the resolved runtime storage client.
