import { BackupErrorCategory } from '@/portainer/services/api/backup.service';

const categoryHints: Record<BackupErrorCategory, string> = {
  invalid_configuration: 'Review the S3 backup settings and required fields.',
  authentication: 'Check the configured access key and secret.',
  bucket_access: 'Check the bucket name and credentials’ bucket permissions.',
  network: 'Check the S3 endpoint and network connectivity.',
  timeout: 'Check the S3 endpoint responsiveness and network connectivity.',
  archive: 'Check available disk space and Portainer logs.',
  upload: 'Check the bucket permissions and endpoint configuration.',
  internal: 'Review Portainer logs for more information.',
};

export function connectionTestHint(category?: string) {
  return category && category in categoryHints
    ? categoryHints[category as BackupErrorCategory]
    : 'Review the backup configuration and try again.';
}
