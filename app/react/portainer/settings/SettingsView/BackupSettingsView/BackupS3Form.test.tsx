import { http, HttpResponse } from 'msw';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { server } from '@/setup-tests/server';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { BackupHealthCenter } from './BackupHealthCenter';
import { BackupS3Form } from './BackupS3Form';

const testUrl = '/api/backup/s3/test';
const settingsUrl = '/api/backup/s3/settings';

function renderS3Settings() {
  const Wrapped = withTestQueryProvider(() => (
    <>
      <BackupS3Form />
      <BackupHealthCenter />
    </>
  ));
  return render(<Wrapped />);
}

test('refreshes health status after export success', async () => {
  let statusRequests = 0;
  server.use(
    http.get('/api/backup/s3/status', () => {
      statusRequests += 1;
      return HttpResponse.json({
        Failed: false,
        TimestampUTC: '',
        state: 'neverRun',
        history: [],
        historyAvailable: true,
      });
    }),
    http.get(settingsUrl, () =>
      HttpResponse.json({
        accessKeyID: 'key-id',
        secretAccessKeyConfigured: true,
        region: 'test-region',
        bucketName: 'test-bucket',
        cronRule: '',
      })
    ),
    http.post('/api/backup/s3/execute', () => HttpResponse.json({}))
  );

  renderS3Settings();
  await screen.findByText('No backup attempts yet.');
  expect(statusRequests).toBe(1);
  fireEvent.click(await screen.findByRole('button', { name: 'Export backup' }));
  await screen.findByText('Backup export completed successfully.');
  await waitFor(() => expect(statusRequests).toBe(2));
});

test('refreshes health status after settings save', async () => {
  let statusRequests = 0;
  server.use(
    http.get('/api/backup/s3/status', () => {
      statusRequests += 1;
      return HttpResponse.json({
        Failed: false,
        TimestampUTC: '',
        state: 'neverRun',
        history: [],
        historyAvailable: true,
      });
    }),
    http.get(settingsUrl, () =>
      HttpResponse.json({
        accessKeyID: 'key-id',
        secretAccessKeyConfigured: true,
        region: 'test-region',
        bucketName: 'test-bucket',
        cronRule: '',
      })
    ),
    http.post(settingsUrl, () => HttpResponse.json({}))
  );

  renderS3Settings();
  await screen.findByText('No backup attempts yet.');
  expect(statusRequests).toBe(1);
  fireEvent.click(
    await screen.findByRole('button', { name: 'Save backup settings' })
  );
  await waitFor(() => expect(statusRequests).toBe(2));
});

test('clears a prior connection-test result when form values change', async () => {
  server.use(
    http.get(settingsUrl, () =>
      HttpResponse.json({
        accessKeyID: 'key-id',
        secretAccessKeyConfigured: true,
        region: 'test-region',
        bucketName: 'test-bucket',
        s3CompatibleHost: '',
        cronRule: '',
      })
    ),
    http.post(testUrl, () =>
      HttpResponse.json({ success: true, category: 'bucket_access_verified' })
    )
  );

  const Wrapped = withTestQueryProvider(BackupS3Form);
  render(<Wrapped />);
  fireEvent.click(
    await screen.findByRole('button', { name: 'Test connection' })
  );
  expect(await screen.findByText(/Bucket access verified/)).toBeVisible();
  fireEvent.change(screen.getByLabelText('Region'), {
    target: { value: 'changed-region' },
  });
  await waitFor(() =>
    expect(screen.queryByText(/Bucket access verified/)).not.toBeInTheDocument()
  );
});

test('allows connection testing when unrelated form fields are invalid', async () => {
  server.use(
    http.get(settingsUrl, () =>
      HttpResponse.json({
        accessKeyID: 'key-id',
        secretAccessKeyConfigured: true,
        region: 'test-region',
        bucketName: 'test-bucket',
        s3CompatibleHost: '',
        cronRule: 'invalid cron',
      })
    ),
    http.post(testUrl, () =>
      HttpResponse.json({ success: true, category: 'bucket_access_verified' })
    )
  );

  const Wrapped = withTestQueryProvider(BackupS3Form);
  render(<Wrapped />);

  expect(
    await screen.findByText('Please enter a valid cron rule.')
  ).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
  expect(await screen.findByText(/Bucket access verified/)).toBeVisible();
});

test('tests current form values without saving and safely renders the result', async () => {
  let settingsPosts = 0;
  let testPayload: unknown;
  server.use(
    http.get(settingsUrl, () =>
      HttpResponse.json({
        accessKeyID: 'key-id',
        secretAccessKey: '',
        secretAccessKeyConfigured: true,
        region: 'test-region',
        bucketName: 'test-bucket',
        s3CompatibleHost: '',
        cronRule: '',
      })
    ),
    http.post(settingsUrl, () => {
      settingsPosts += 1;
      return HttpResponse.json({});
    }),
    http.post(testUrl, async ({ request }) => {
      testPayload = await request.json();
      return HttpResponse.json({
        success: false,
        category: 'unknown-danger',
        message: 'do not show raw response message',
      });
    })
  );

  const Wrapped = withTestQueryProvider(BackupS3Form);
  render(<Wrapped />);

  fireEvent.click(
    await screen.findByRole('button', { name: 'Test connection' })
  );
  expect(await screen.findByText(/Connection test failed\./)).toBeVisible();
  expect(
    screen.getByText(/Review the backup configuration and try again/)
  ).toBeVisible();
  expect(
    screen.queryByText(/do not show raw response message/)
  ).not.toBeInTheDocument();
  expect(testPayload).toEqual({
    accessKeyID: 'key-id',
    secretAccessKey: '',
    region: 'test-region',
    bucketName: 'test-bucket',
    s3CompatibleHost: '',
  });
  expect(settingsPosts).toBe(0);
});
