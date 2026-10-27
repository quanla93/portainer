import { Formik, Form, Field } from 'formik';
import { CheckCircle, Upload } from 'lucide-react';

import { success as notifySuccess } from '@/portainer/services/notifications';

import { FormControl } from '@@/form-components/FormControl';
import { LoadingButton } from '@@/buttons/LoadingButton';
import { Input } from '@@/form-components/Input';
import { SwitchField } from '@@/form-components/SwitchField';

import {
  useBackupS3Settings,
  useExportS3BackupMutation,
  useUpdateBackupS3SettingsMutation,
  useTestBackupS3Connection,
} from './queries';
import { BackupS3Model, BackupS3Settings } from './types';
import { validationSchema } from './BackupS3Form.validation';
import { SecurityFieldset } from './SecurityFieldset';
import { connectionTestHint } from './backupHealthText';

export function BackupS3Form() {
  const exportS3Mutate = useExportS3BackupMutation();
  const updateS3Mutate = useUpdateBackupS3SettingsMutation();
  const testConnectionMutate = useTestBackupS3Connection();
  const settingsQuery = useBackupS3Settings();

  if (settingsQuery.isInitialLoading) {
    return null;
  }

  const settings = settingsQuery.data;
  const secretAccessKeyConfigured = !!settings?.secretAccessKeyConfigured;
  const passwordConfigured = !!settings?.passwordConfigured;
  const backupS3Settings = {
    password: settings?.password || '',
    cronRule: settings?.cronRule || '',
    accessKeyID: settings?.accessKeyID || '',
    secretAccessKey: settings?.secretAccessKey || '',
    region: settings?.region || '',
    bucketName: settings?.bucketName || '',
    s3CompatibleHost: settings?.s3CompatibleHost || '',
    scheduleAutomaticBackup: !!settings?.cronRule,
    passwordProtect: passwordConfigured,
  };

  return (
    <Formik<BackupS3Settings>
      initialValues={backupS3Settings}
      validationSchema={validationSchema({
        secretAccessKeyConfigured,
        passwordConfigured,
      })}
      onSubmit={onSubmit}
      validateOnMount
    >
      {({ values, errors, setFieldValue, isValid }) => {
        const canTestConnection =
          !!values.accessKeyID.trim() &&
          !!values.bucketName.trim() &&
          (!!values.secretAccessKey.trim() || secretAccessKeyConfigured) &&
          (!values.s3CompatibleHost ||
            /^https?:\/\//.test(values.s3CompatibleHost));

        return (
          <Form
            className="form-horizontal"
            onChange={() => testConnectionMutate.reset()}
          >
            <div className="form-group">
              <div className="col-sm-12">
                <SwitchField
                  name="schedule-automatic-backup"
                  data-cy="settings-scheduleAutomaticBackupSwitch"
                  labelClass="col-sm-3 col-lg-2"
                  label="Schedule automatic backups"
                  checked={values.scheduleAutomaticBackup}
                  onChange={(value) =>
                    setFieldValue('scheduleAutomaticBackup', value)
                  }
                />
              </div>
            </div>

            {values.scheduleAutomaticBackup && (
              <FormControl
                inputId="cron_rule"
                label="Cron rule"
                size="small"
                errors={errors.cronRule}
                required
              >
                <Field
                  id="cron_rule"
                  name="cronRule"
                  type="text"
                  as={Input}
                  placeholder="0 2 * * *"
                  data-cy="settings-backupCronRuleInput"
                />
              </FormControl>
            )}

            <FormControl
              label="Access key ID"
              inputId="access_key_id"
              errors={errors.accessKeyID}
            >
              <Field
                id="access_key_id"
                name="accessKeyID"
                type="text"
                as={Input}
                data-cy="settings-accessKeyIdInput"
              />
            </FormControl>

            <FormControl
              label="Secret access key"
              inputId="secret_access_key"
              errors={errors.secretAccessKey}
              tooltip={
                secretAccessKeyConfigured
                  ? 'A secret access key is already saved. Leave this field blank to keep it, or enter a new value to replace it.'
                  : undefined
              }
            >
              <Field
                id="secret_access_key"
                name="secretAccessKey"
                type="password"
                as={Input}
                placeholder={
                  secretAccessKeyConfigured
                    ? 'Leave blank to keep the saved secret'
                    : undefined
                }
                data-cy="settings-secretAccessKeyInput"
              />
            </FormControl>

            <FormControl label="Region" inputId="region" errors={errors.region}>
              <Field
                id="region"
                name="region"
                type="text"
                as={Input}
                placeholder="us-east-1 for AWS, auto for Cloudflare R2"
                data-cy="settings-backupRegionInput"
              />
            </FormControl>

            <FormControl
              label="Bucket name"
              inputId="bucket_name"
              errors={errors.bucketName}
            >
              <Field
                id="bucket_name"
                name="bucketName"
                type="text"
                as={Input}
                data-cy="settings-backupBucketNameInput"
              />
            </FormControl>

            <FormControl
              label="S3 compatible host"
              inputId="s3_compatible_host"
              tooltip="Leave empty for AWS S3. For Cloudflare R2, use https://<account-id>.r2.cloudflarestorage.com. This field also supports MinIO, Wasabi, Backblaze B2, and other S3-compatible services."
              errors={errors.s3CompatibleHost}
            >
              <Field
                id="s3_compatible_host"
                name="s3CompatibleHost"
                type="text"
                as={Input}
                placeholder="https://<account-id>.r2.cloudflarestorage.com"
                data-cy="settings-backupS3CompatibleHostInput"
              />
            </FormControl>

            <SecurityFieldset
              switchDataCy="settings-passwordProtectToggleS3"
              inputDataCy="settings-backups3pw"
              passwordConfigured={passwordConfigured}
            />

            <div className="form-group">
              <div className="col-sm-12 flex flex-wrap gap-2">
                <LoadingButton
                  type="button"
                  loadingText="Testing connection..."
                  isLoading={testConnectionMutate.isLoading}
                  className="!ml-0"
                  disabled={
                    !canTestConnection || testConnectionMutate.isLoading
                  }
                  data-cy="settings-testBackupS3ConnectionButton"
                  onClick={() =>
                    testConnectionMutate.mutate({
                      accessKeyID: values.accessKeyID,
                      secretAccessKey: values.secretAccessKey,
                      region: values.region,
                      bucketName: values.bucketName,
                      s3CompatibleHost: values.s3CompatibleHost,
                    })
                  }
                >
                  Test connection
                </LoadingButton>
                <LoadingButton
                  type="button"
                  loadingText="Exporting..."
                  isLoading={exportS3Mutate.isLoading}
                  className="!ml-0"
                  disabled={!isValid || exportS3Mutate.isLoading}
                  data-cy="settings-exportBackupS3Button"
                  icon={Upload}
                  onClick={() => handleExport(values)}
                >
                  Export backup
                </LoadingButton>
              </div>
            </div>
            <div className="form-group">
              <div className="col-sm-12">
                <LoadingButton
                  loadingText="Saving settings..."
                  isLoading={updateS3Mutate.isLoading}
                  className="!ml-0"
                  disabled={!isValid || updateS3Mutate.isLoading}
                  data-cy="settings-saveBackupSettingsButton"
                >
                  Save backup settings
                </LoadingButton>
              </div>
            </div>
            {testConnectionMutate.data && (
              <div
                role={
                  testConnectionMutate.data.category === 'timeout' ||
                  testConnectionMutate.data.category === 'network'
                    ? 'alert'
                    : 'status'
                }
                className="form-group"
              >
                <div className="col-sm-12">
                  {testConnectionMutate.data.success ? (
                    <p>
                      <CheckCircle aria-hidden="true" className="mr-1 inline" />
                      Bucket access verified. This does not verify upload
                      permission.
                    </p>
                  ) : (
                    <p>
                      Connection test failed.{' '}
                      {connectionTestHint(testConnectionMutate.data.category)}
                    </p>
                  )}
                </div>
              </div>
            )}
            {exportS3Mutate.isLoading && (
              <p role="status">Export is in progress...</p>
            )}
            {exportS3Mutate.isSuccess && (
              <p role="status">Backup export completed successfully.</p>
            )}
            {exportS3Mutate.isError && (
              <p role="alert">
                Backup export failed. Check the configuration and try again.
              </p>
            )}
          </Form>
        );
      }}
    </Formik>
  );

  function handleExport(values: BackupS3Settings) {
    const payload = toBackupS3Model(values);
    exportS3Mutate.mutate(payload, {
      onSuccess() {
        notifySuccess('Success', 'Exported backup to S3 successfully');
      },
    });
  }

  async function onSubmit(values: BackupS3Settings) {
    updateS3Mutate.mutate(toBackupS3Model(values), {
      onSuccess() {
        notifySuccess('Success', 'S3 backup settings saved successfully');
      },
    });
  }
}

function toBackupS3Model(values: BackupS3Settings): BackupS3Model {
  return {
    password: values.passwordProtect ? values.password : '',
    cronRule: values.scheduleAutomaticBackup ? values.cronRule : '',
    accessKeyID: values.accessKeyID,
    secretAccessKey: values.secretAccessKey,
    region: values.region,
    bucketName: values.bucketName,
    s3CompatibleHost: values.s3CompatibleHost,
  };
}
