export const queryKeys = {
  base: () => ['settings'] as const,
  backupS3Settings: () => [...queryKeys.base(), 'backupS3Settings'] as const,
  backupS3Status: () => ['backup', 'status'] as const,
};
