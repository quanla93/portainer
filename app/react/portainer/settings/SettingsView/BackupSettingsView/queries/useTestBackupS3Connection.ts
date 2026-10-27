import { useMutation } from '@tanstack/react-query';

import { testBackupS3Connection } from '@/portainer/services/api/backup.service';
import { withError } from '@/react-tools/react-query';

export function useTestBackupS3Connection() {
  return useMutation(testBackupS3Connection, {
    ...withError('Unable to test S3 backup connection'),
  });
}
