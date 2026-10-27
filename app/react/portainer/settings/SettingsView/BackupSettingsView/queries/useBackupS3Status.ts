import { useQuery } from '@tanstack/react-query';

import { getBackupStatus } from '@/portainer/services/api/backup.service';
import { withError } from '@/react-tools/react-query';

import { queryKeys } from './queryKeys';

export function useBackupS3Status(enabled = true) {
  return useQuery(queryKeys.backupS3Status(), getBackupStatus, {
    enabled,
    refetchOnWindowFocus: false,
    ...withError('Unable to retrieve S3 backup status'),
  });
}
