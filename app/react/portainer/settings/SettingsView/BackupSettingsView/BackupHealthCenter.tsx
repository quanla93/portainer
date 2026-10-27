import { useMemo } from 'react';
import { RefreshCw } from 'lucide-react';

import { BackupStatus } from '@/portainer/services/api/backup.service';

import { connectionTestHint } from './backupHealthText';

import { LoadingButton } from '@@/buttons/LoadingButton';
import { Widget, WidgetBody, WidgetTitle } from '@@/Widget';

import { useBackupS3Status } from './queries';

function safeErrorHint(category?: string) {
  return connectionTestHint(category);
}

export function BackupHealthCenter() {
  const statusQuery = useBackupS3Status();
  const status = statusQuery.data;
  const attempts = useMemo(
    () => (status?.history ? [...status.history] : []),
    [status?.history]
  );

  return (
    <section aria-label="S3 backup health center">
      <Widget>
        <WidgetTitle title="S3 backup health" />
        <WidgetBody>
          <div className="flex items-center justify-between gap-2">
            <p>Backup status and recent export attempts.</p>
            <LoadingButton
              type="button"
              isLoading={statusQuery.isFetching}
              loadingText="Refreshing..."
              icon={RefreshCw}
              onClick={() => statusQuery.refetch()}
              aria-label="Refresh backup health"
              data-cy="settings-refreshBackupHealthButton"
            >
              Refresh
            </LoadingButton>
          </div>

          {statusQuery.isLoading && (
            <p role="status">Loading backup health...</p>
          )}
          {statusQuery.isError && (
            <div role="alert">
              Unable to load backup health.{' '}
              <button type="button" onClick={() => statusQuery.refetch()}>
                Try again
              </button>
            </div>
          )}

          {status && <StatusDetails status={status} attempts={attempts} />}
        </WidgetBody>
      </Widget>
    </section>
  );
}

function StatusDetails({
  status,
  attempts,
}: {
  status: BackupStatus;
  attempts: NonNullable<BackupStatus['history']>;
}) {
  const state = status.state;
  const hasHistory = Object.prototype.hasOwnProperty.call(status, 'history');

  return (
    <div>
      {state === 'running' && <p role="status">A backup export is running.</p>}
      {state === 'succeeded' && (
        <p role="status">Latest backup completed successfully.</p>
      )}
      {state === 'failed' && (
        <div role="status" aria-live="polite">
          <p>
            Latest backup failed
            {status.TimestampUTC &&
              ` at ${formatLocalTime(status.TimestampUTC)}`}
            .
          </p>
          {status.latestErrorMessage && <p>{status.latestErrorMessage}</p>}
          <p>{safeErrorHint(status.latestErrorCategory)}</p>
        </div>
      )}
      {state === 'unavailable' && <p>Latest backup status is unavailable.</p>}
      {state === 'neverRun' && !hasHistory && <p>No backup attempts yet.</p>}
      {!state && status.Failed && (
        <p>Latest backup failed at {formatLocalTime(status.TimestampUTC)}.</p>
      )}
      {!state && !status.Failed && status.TimestampUTC && (
        <p>
          Latest backup status updated at {formatLocalTime(status.TimestampUTC)}
          .
        </p>
      )}

      {status.historyAvailable === false && (
        <p role="alert">
          Backup history is unavailable; recent attempts may be incomplete.
        </p>
      )}
      {hasHistory &&
        status.historyAvailable !== false &&
        attempts.length === 0 && <p>No backup attempts yet.</p>}
      {attempts.length > 0 && (
        <div>
          <h3>Recent backup attempts</h3>
          <ul>
            {attempts.map((attempt) => (
              <li key={attempt.id}>
                <strong>
                  {attempt.outcome === 'succeeded' ? 'Succeeded' : 'Failed'}
                </strong>{' '}
                ({attempt.trigger}) — started{' '}
                {formatLocalTime(attempt.startedAtUTC)}
                {', completed '}
                {formatLocalTime(attempt.completedAtUTC)}; {attempt.durationMS}{' '}
                ms
                {attempt.outcome === 'failed' && (
                  <>
                    {attempt.safeErrorMessage && (
                      <p>{attempt.safeErrorMessage}</p>
                    )}
                    <p>{safeErrorHint(attempt.errorCategory)}</p>
                  </>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {status.schedule && <ScheduleDetails schedule={status.schedule} />}
    </div>
  );
}

function ScheduleDetails({
  schedule,
}: {
  schedule: NonNullable<BackupStatus['schedule']>;
}) {
  if (schedule.state === 'disabled' || schedule.enabled === false) {
    return <p>Automatic backups are off.</p>;
  }
  if (schedule.state === 'invalid') {
    return <p>The automatic backup schedule is invalid.</p>;
  }
  if (schedule.state === 'unavailable') {
    return <p>The automatic backup schedule is unavailable.</p>;
  }

  return (
    <div>
      <h3>Automatic backup schedule</h3>
      {schedule.nextRunUTC && (
        <p>
          Next calculated run: {formatLocalTime(schedule.nextRunUTC)} (browser
          local time; execution is not guaranteed).
        </p>
      )}
      {schedule.timezoneState === 'available' && schedule.timezone && (
        <p>Server scheduler timezone: {schedule.timezone}</p>
      )}
      {schedule.timezoneState === 'unavailable' && (
        <p>
          Server scheduler timezone is unavailable; next run is shown in
          browser-local time only.
        </p>
      )}
    </div>
  );
}

function formatLocalTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : `${date.toLocaleString()} (browser local time)`;
}
