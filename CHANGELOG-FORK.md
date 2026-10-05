# Portainer fork changelog

This file tracks changes specific to the `quanla93/portainer` CE fork. Upstream Portainer release notes remain authoritative for changes not listed here.

## 2.45.1 upstream integration

### Known issues

- None known at release time.

### Changes

- Integrated the Portainer `2.45.1` upstream snapshot (`bcfb8d279`), including security and dependency updates, FIPS checks, Helm and GitOps fixes, registry cache fixes, and Kubernetes/UI improvements.
- Carried forward the fork's CE customizations for OAuth/local accounts, RBAC role management, and stack/container webhooks.
- Built release images from this tag's complete source snapshot; this version includes all changes committed before the release tag, not only the commit that introduced the tag.

## 2.44.2-ce-unlocked

- Fixed local-user authentication visibility by exposing `UserHasPassword` in user listings and preventing it from being hidden in user inspect responses.

## 2.44.1-ce-unlocked

- Added local accounts with password authentication while OAuth or LDAP authentication is configured.
- Updated account UI to show the correct authentication method and password-change controls for local accounts.

## 2.44.0-ce-unlocked

- Enabled selected business OAuth functionality in the CE fork.
- Enabled stack and container webhooks and RBAC role management in CE, including role access-table UI and access-service mapping.
- Added fork release and Docker image automation for version tags.
