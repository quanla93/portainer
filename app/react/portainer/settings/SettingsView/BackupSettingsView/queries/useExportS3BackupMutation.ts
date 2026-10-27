import { useMutation, useQueryClient } from '@tanstack/react-query';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { withError } from '@/react-tools/react-query';

import { BackupS3Model } from '../types';

import { buildUrl } from './backupSettings.service';

export function useExportS3BackupMutation() {
  const queryClient = useQueryClient();

  return useMutation(exportS3Backup, {
    onSettled: () => queryClient.invalidateQueries(['backup', 'status']),
    ...withError('Unable to export backup to S3'),
  });
}

async function exportS3Backup(payload: BackupS3Model) {
  try {
    const response = await axios.post(buildUrl('s3', 'execute'), payload, {});

    return response.data;
  } catch (e) {
    throw parseAxiosError(e as Error, 'Unable to export s3 backup');
  }
}
