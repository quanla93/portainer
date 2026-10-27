import axios, { parseAxiosError } from '../axios/axios';

export type BackupStatusState =
  | 'neverRun'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'unavailable';

export type BackupErrorCategory =
  | 'invalid_configuration'
  | 'authentication'
  | 'bucket_access'
  | 'network'
  | 'timeout'
  | 'archive'
  | 'upload'
  | 'internal';

export interface BackupAttempt {
  id: number;
  trigger: 'manual' | 'scheduled';
  startedAtUTC: string;
  completedAtUTC: string;
  durationMS: number;
  outcome: 'succeeded' | 'failed';
  errorCategory?: string;
  safeErrorMessage?: string;
}

export interface BackupSchedule {
  enabled?: boolean;
  state?: 'enabled' | 'disabled' | 'invalid' | 'unavailable' | string;
  nextRunUTC?: string;
  timezoneState?: 'available' | 'unavailable' | string;
  timezone?: string;
}

export interface BackupStatus {
  Failed: boolean;
  TimestampUTC: string;
  state?: BackupStatusState;
  latestTrigger?: string;
  runningTrigger?: string;
  latestErrorCategory?: string;
  latestErrorMessage?: string;
  history?: BackupAttempt[];
  historyAvailable?: boolean;
  schedule?: BackupSchedule;
}

export interface BackupConnectionTestResult {
  success: boolean;
  category: string;
  message?: string;
}

export interface BackupS3TestSettings {
  accessKeyID: string;
  secretAccessKey: string;
  region: string;
  bucketName: string;
  s3CompatibleHost: string;
}

export async function getBackupStatus() {
  try {
    const { data } = await axios.get<BackupStatus>(buildUrl('s3', 'status'));
    return data;
  } catch (error) {
    throw parseAxiosError(error as Error, 'Unable to retrieve backup status');
  }
}

export async function testBackupS3Connection(settings: BackupS3TestSettings) {
  try {
    const { data } = await axios.post<BackupConnectionTestResult>(
      buildUrl('s3', 'test'),
      settings
    );
    return data;
  } catch (error) {
    if (typeof error === 'object' && error !== null) {
      if (
        'code' in error &&
        (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT')
      ) {
        return {
          success: false,
          category: 'timeout',
        };
      }
      if ('response' in error && error.response) {
        return {
          success: false,
          category: 'internal',
        };
      }
    }
    return {
      success: false,
      category: 'network',
    };
  }
}

function buildUrl(resource?: string, action?: string) {
  let url = '/backup';

  if (resource) {
    url += `/${resource}`;
  }

  if (action) {
    url += `/${action}`;
  }

  return url;
}
