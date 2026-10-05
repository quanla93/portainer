# Portainer fork changelog

This file tracks changes specific to the `quanla93/portainer` CE fork. Upstream Portainer release notes remain authoritative for changes not listed here.

## 2.45.1 upstream integration

- Integrated upstream Portainer `2.45.1` (`bcfb8d279`), including upstream security/dependency updates, FIPS checks, Helm fixes, GitOps updates, registry cache fixes, and Kubernetes/UI fixes.
- Preserved the fork's CE-unlocked OAuth/local-account, RBAC role, and webhook changes.

## 2.44.2-ce-unlocked

- Fixed local-user authentication visibility by exposing `UserHasPassword` in user listings and preventing it from being hidden in user inspect responses.

## 2.44.1-ce-unlocked

- Added local accounts with password authentication while OAuth or LDAP authentication is configured.
- Updated account UI to show the correct authentication method and password-change controls for local accounts.

## 2.44.0-ce-unlocked

- Enabled selected business OAuth functionality in the CE fork.
- Enabled stack and container webhooks and RBAC role management in CE, including role access-table UI and access-service mapping.
- Added fork release and Docker image automation for version tags.
