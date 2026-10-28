# Portainer fork changelog

This file tracks changes specific to the `quanla93/portainer` CE fork. Upstream Portainer release notes remain authoritative for changes not listed here.

## 2.45.3-ce-r2.3

### Fixes

- Restore fork-specific API customizations required by the CE build, including OAuth team mappings, local-password visibility, container webhooks, and internal-auth settings.

## 2.45.3-ce-r2.2

### Fixes

- Restore the backend backup models required by the Backup Health Center so the Go server build completes.

## 2.45.3-ce-r2.1

### Changes

- Integrated the upstream Portainer `2.45.3` source tag. See the [official Portainer 2.45.3 release notes](https://github.com/portainer/portainer/releases/tag/2.45.3) for upstream changes.
- Retained the fork's Backup Health Center changes, including bounded backup-attempt history, S3-compatible bucket connectivity testing, backup outcome tracking, and next scheduled backup reporting.

## 2.45.2-ce-r2.1

### Changes

- Integrated the upstream Portainer `2.45.2` source tag. See the [official Portainer 2.45.2 release notes](https://github.com/portainer/portainer/releases/tag/2.45.2) for upstream changes.
- Added persistent, bounded backup-attempt history and an S3-compatible bucket connection test for the Backup Health Center.
- Recorded manual and scheduled S3 backup outcomes, duration, trigger, and safe error summaries in the status API.
- Added the calculated next scheduled backup time in UTC with timezone availability; disabled or invalid schedules report no next-run time. Existing backup status fields remain available.

## 2.45.1-ce-r2.2

### Changes

- Enabled scheduled S3-compatible Portainer backups in Community Edition.
- Added support for Cloudflare R2, AWS S3, MinIO, Wasabi, Backblaze B2 S3 API, and other S3-compatible endpoints.
- Added Cloudflare R2 endpoint guidance, five-field cron scheduling, backup status reporting, and credential redaction from the settings API.

## Release-note convention

For a curated GitHub release body, add a section named `## <version>` before pushing the matching `v<version>` tag (for example, `## 2.45.1-ce-r2.2`). If it is absent or empty, release automation generates notes from commits since the preceding version tag so image builds and release publication can still complete.

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
