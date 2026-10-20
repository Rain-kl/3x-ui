# ACME HTTP-01 listener on a non-standard port

The panel's acme.sh standalone listener defaults to `65800` instead of `80`
for domain certificates, IP certificates, manual renewal and automatic
renewal. `XUI_ACME_HTTP_PORT` overrides the default. Let's Encrypt still
validates HTTP-01 on external port 80, so port 80 must forward to the
configured listener (port mapping or a reverse proxy for
`/.well-known/acme-challenge/`). The scripts print that requirement whenever a
non-80 port is used.

acme.sh persists the listener in each certificate's `Le_HTTPPort` conf field
and ignores `--httpport` when `--renew` reloads that conf. Issuance therefore
writes the chosen port, and renewal paths rewrite it before renewing:

- Opening the SSL menu migrates certificates saved on port 80 (or with no
  `Le_HTTPPort`) to the configured port, leaving intentionally custom ports
  alone.
- Force Renew rewrites the selected certificate to the configured port before
  `acme.sh --renew`.
- Reusing an existing certificate during issuance rewrites it to the port the
  user chose in that run.

## Acceptance scenarios

- A domain or IP certificate is issued with the listener on 65800 by default;
  its acme.sh conf carries `Le_HTTPPort='65800'`.
- Entering the SSL menu rewrites `Le_HTTPPort='80'` and missing values to the
  configured port, but not a certificate saved on a custom port such as 8443.
- Force Renew writes `Le_HTTPPort` before calling `--renew`, so the renewed
  certificate keeps using the non-standard port; the cron renewal reads the
  same value.
- `XUI_ACME_HTTP_PORT=12345` replaces 65800 for new issuance, Force Renew and
  the bulk migration.
- Choosing a different port during issuance asks for external port 80 to be
  forwarded to that port and persists it for renewal.
