import { http, HttpResponse } from 'msw';
import { fireEvent, render, screen } from '@testing-library/react';

import { server } from '@/setup-tests/server';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { BackupHealthCenter } from './BackupHealthCenter';

const statusUrl = '/api/backup/s3/status';

function renderCenter() {
  const Wrapped = withTestQueryProvider(BackupHealthCenter);
  return render(<Wrapped />);
}

test('renders attempts, safe failure guidance, schedule and supports explicit refresh', async () => {
  let requests = 0;
  server.use(
    http.get(statusUrl, () => {
      requests += 1;
      return HttpResponse.json({
        Failed: true,
        TimestampUTC: '2026-10-13T10:00:00Z',
        state: 'failed',
        latestErrorCategory: 'authentication',
        latestErrorMessage: 'Saved safe summary.',
        historyAvailable: true,
        history: [
          {
            id: 1,
            trigger: 'scheduled',
            startedAtUTC: '2026-10-13T09:00:00Z',
            completedAtUTC: '2026-10-13T09:01:00Z',
            durationMS: 60000,
            outcome: 'failed',
            errorCategory: 'authentication',
            safeErrorMessage: 'Safe per-attempt summary.',
            rawError: 'secret=super-secret raw provider diagnostic',
          },
        ],
        schedule: {
          enabled: true,
          state: 'enabled',
          nextRunUTC: '2026-10-14T02:00:00Z',
          timezoneState: 'available',
          timezone: 'UTC',
        },
      });
    })
  );

  renderCenter();
  expect(await screen.findByText(/Latest backup failed/)).toBeVisible();
  expect(
    screen.getAllByText('Check the configured access key and secret.')
  ).toHaveLength(2);
  expect(screen.getByText('Saved safe summary.')).toBeVisible();
  expect(screen.getByText('Safe per-attempt summary.')).toBeVisible();
  expect(screen.getByText(/scheduled/)).toBeVisible();
  expect(screen.getByText(/Server scheduler timezone: UTC/)).toBeVisible();
  expect(
    screen.queryByText(/raw server detail|super-secret|raw provider diagnostic/)
  ).not.toBeInTheDocument();
  expect(requests).toBe(1);

  fireEvent.click(
    screen.getByRole('button', { name: 'Refresh backup health' })
  );
  await screen.findByText(/Next calculated run/);
  expect(requests).toBe(2);
});

test('renders explicit empty, history unavailable, running, schedule-unavailable and timezone-unavailable states', async () => {
  server.use(
    http.get(statusUrl, () =>
      HttpResponse.json({
        Failed: false,
        TimestampUTC: '',
        state: 'running',
        historyAvailable: false,
        history: [],
        schedule: {
          enabled: true,
          state: 'unavailable',
          timezoneState: 'unavailable',
        },
      })
    )
  );
  const { unmount } = renderCenter();
  expect(await screen.findByText('A backup export is running.')).toBeVisible();
  expect(screen.getByText(/history is unavailable/i)).toBeVisible();
  expect(screen.getByText(/schedule is unavailable/i)).toBeVisible();
  unmount();

  server.use(
    http.get(statusUrl, () =>
      HttpResponse.json({
        Failed: false,
        TimestampUTC: '',
        state: 'neverRun',
        historyAvailable: true,
        history: [],
        schedule: {
          enabled: true,
          state: 'enabled',
          nextRunUTC: '2026-10-14T02:00:00Z',
          timezoneState: 'unavailable',
        },
      })
    )
  );
  renderCenter();
  expect(await screen.findByText('No backup attempts yet.')).toBeVisible();
  expect(screen.getByText(/timezone is unavailable/)).toBeVisible();
  expect(screen.getByText(/browser-local time only/)).toBeVisible();
});

test('renders legacy-only status without fabricated history or schedule', async () => {
  server.use(
    http.get(statusUrl, () =>
      HttpResponse.json({ Failed: true, TimestampUTC: '2026-10-13T10:00:00Z' })
    )
  );

  renderCenter();
  expect(await screen.findByText(/Latest backup failed at/)).toBeVisible();
  expect(screen.queryByText('No backup attempts yet.')).not.toBeInTheDocument();
  expect(
    screen.queryByText('Automatic backup schedule')
  ).not.toBeInTheDocument();
});
