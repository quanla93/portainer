package backup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/adminmonitor"
	operations "github.com/portainer/portainer/api/backup"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/stretchr/testify/require"
)

func TestScheduledExportUsesLifecycleContext(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newTestBackupHandler(t, store)
	h.SetShutdownContext(ctx)
	var receivedContext context.Context
	h.uploadS3Backup = func(ctx context.Context, _ portainer.S3BackupSettings) error {
		receivedContext = ctx
		return nil
	}

	err = h.runScheduledExport(portainer.S3BackupSettings{})

	require.NoError(t, err)
	require.Same(t, ctx, receivedContext)
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "scheduled", attempts[0].Trigger)
}

func TestS3StatusAddsNextRunWithoutChangingBackupLegacyFields(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	settings, err := store.Settings().Settings()
	require.NoError(t, err)
	settings.S3BackupSettings.CronRule = "0 2 * * *"
	require.NoError(t, store.Settings().UpdateSettings(settings))
	now := time.Date(2026, 10, 12, 10, 30, 0, 0, time.UTC)
	h := newTestBackupHandler(t, store)
	h.now = func() time.Time { return now }
	local := time.Local
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	time.Local = location
	t.Cleanup(func() { time.Local = local })
	h.statusMu.Lock()
	h.backupStatus = portainer.BackupStatus{Failed: true, TimestampUTC: "2026-10-12T09:00:00Z", State: "failed", LatestTrigger: "manual"}
	h.statusMu.Unlock()

	status := h.getS3BackupStatus()

	require.Equal(t, true, status.Schedule.Enabled)
	require.Equal(t, "enabled", status.Schedule.State)
	require.Equal(t, "2026-10-13T06:00:00Z", status.Schedule.NextRunUTC)
	require.Equal(t, "available", status.Schedule.TimezoneState)
	require.Equal(t, "America/New_York", status.Schedule.Timezone)
	require.True(t, status.Failed)
	require.Equal(t, "2026-10-12T09:00:00Z", status.TimestampUTC)
	require.Equal(t, "failed", status.State)
}

func TestS3StatusHandlesDisabledAndInvalidSchedules(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	h.now = func() time.Time { return time.Date(2026, 10, 12, 10, 30, 0, 0, time.UTC) }
	local := time.Local
	time.Local = time.FixedZone("unresolvable", 0)
	t.Cleanup(func() { time.Local = local })

	status := h.getS3BackupStatus()
	require.Equal(t, "disabled", status.Schedule.State)
	require.Empty(t, status.Schedule.NextRunUTC)

	settings, err := store.Settings().Settings()
	require.NoError(t, err)
	settings.S3BackupSettings.CronRule = "not a cron rule"
	require.NoError(t, store.Settings().UpdateSettings(settings))
	status = h.getS3BackupStatus()
	require.Equal(t, "invalid", status.Schedule.State)
	require.Empty(t, status.Schedule.NextRunUTC)
}

func TestS3StatusHandlesUnavailableLocalTimezone(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	settings, err := store.Settings().Settings()
	require.NoError(t, err)
	settings.S3BackupSettings.CronRule = "0 2 * * *"
	require.NoError(t, store.Settings().UpdateSettings(settings))
	h := newTestBackupHandler(t, store)
	h.now = func() time.Time { return time.Date(2026, 10, 12, 10, 30, 0, 0, time.UTC) }
	local := time.Local
	time.Local = time.FixedZone("unresolvable", 0)
	t.Cleanup(func() { time.Local = local })

	status := h.getS3BackupStatus()
	require.Equal(t, "enabled", status.Schedule.State)
	require.Equal(t, "2026-10-13T02:00:00Z", status.Schedule.NextRunUTC)
	require.Equal(t, "unavailable", status.Schedule.TimezoneState)
	require.Empty(t, status.Schedule.Timezone)
}

func TestS3StatusPreservesLegacyFieldsAndIncludesHistory(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.BackupHistory().Append(&portainer.BackupAttempt{
		Trigger: "manual", StartedAtUTC: now.Add(-time.Minute), CompletedAtUTC: now, DurationMS: 60000, Outcome: "succeeded",
	}))

	h := newTestBackupHandler(t, store)
	h.statusMu.Lock()
	h.backupStatus = portainer.BackupStatus{Failed: false, TimestampUTC: now.Format(time.RFC3339)}
	h.statusMu.Unlock()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, statusRequest(true))

	require.Equal(t, http.StatusOK, rr.Code)
	var got portainer.BackupStatus
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	require.False(t, got.Failed)
	require.Equal(t, now.Format(time.RFC3339), got.TimestampUTC)
	require.Equal(t, "succeeded", got.State)
	require.Equal(t, "manual", got.LatestTrigger)
	require.Len(t, got.History, 1)
}

func TestS3StatusRunningKeepsLastCompletedLegacyFields(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	h.statusMu.Lock()
	h.backupStatus = portainer.BackupStatus{
		Failed: true, TimestampUTC: "2026-10-12T09:00:00Z", State: "failed", LatestTrigger: "manual",
	}
	h.runningExport = &portainer.BackupAttempt{Trigger: "scheduled"}
	h.statusMu.Unlock()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, statusRequest(true))

	var got portainer.BackupStatus
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	require.Equal(t, "running", got.State)
	require.Equal(t, "scheduled", got.RunningTrigger)
	require.True(t, got.Failed)
	require.Equal(t, "2026-10-12T09:00:00Z", got.TimestampUTC)
}

func TestS3StatusHistoryUnavailableIsNotNeverRun(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	failingStore := &failingHistoryDataStore{DataStore: store, history: failingHistoryService{err: errors.New("history unavailable")}}
	h := newTestBackupHandler(t, failingStore)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, statusRequest(true))

	var got portainer.BackupStatus
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	require.Equal(t, "unavailable", got.State)
	require.False(t, got.HistoryAvailable)
	require.Empty(t, got.History)
	require.Empty(t, got.TimestampUTC)
}

func TestS3StatusNoHistoryIsNeverRun(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, statusRequest(true))

	var got portainer.BackupStatus
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	require.Equal(t, "neverRun", got.State)
	require.Empty(t, got.TimestampUTC)
	require.Empty(t, got.History)
	require.True(t, got.HistoryAvailable)
}

func TestS3StatusRequiresAdmin(t *testing.T) {
	h := newTestBackupHandler(t, testhelpers.NewDatastore())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, statusRequest(false))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestManualExportReturnsSafeErrorAndRecordsFailure(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	var receivedContext context.Context
	h.uploadS3Backup = func(ctx context.Context, _ portainer.S3BackupSettings) error {
		receivedContext = ctx
		return operations.NewS3ExportError("upload", "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", errors.New("raw provider failure secret=do-not-expose"))
	}
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/backup/s3/execute", strings.NewReader(`{"accessKeyID":"key","secretAccessKey":"secret","bucketName":"bucket"}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(security.StoreRestrictedRequestContext(request, &security.RestrictedRequestContext{IsAdmin: true}))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, request)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NotNil(t, receivedContext)
	require.ErrorIs(t, receivedContext.Err(), nil)
	require.NotContains(t, rr.Body.String(), "do-not-expose")
	require.Contains(t, rr.Body.String(), "backup")
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "manual", attempts[0].Trigger)
	require.Equal(t, "upload", attempts[0].ErrorCategory)
}

func TestManualExportRoutePreservesStatusAndSanitizesError(t *testing.T) {
	settings := &portainer.Settings{S3BackupSettings: portainer.S3BackupSettings{
		SecretAccessKey: "saved-secret",
	}}
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	require.NoError(t, store.Settings().UpdateSettings(settings))
	h := newTestBackupHandler(t, store)
	var seenContext context.Context
	h.uploadS3Backup = func(ctx context.Context, got portainer.S3BackupSettings) error {
		seenContext = ctx
		require.Equal(t, "saved-secret", got.SecretAccessKey)
		return operations.NewS3ExportError("upload", "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", errors.New("provider error secret=do-not-expose"))
	}
	body := `{"accessKeyID":"access-key","secretAccessKey":"","bucketName":"bucket"}`
	r := httptest.NewRequest(http.MethodPost, "/backup/s3/execute", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{IsAdmin: true}))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, r)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NotNil(t, seenContext)
	require.NotContains(t, rr.Body.String(), "do-not-expose")
	require.Contains(t, rr.Body.String(), "backup")
	require.True(t, h.backupStatus.Failed)
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "manual", attempts[0].Trigger)
}

func TestExportS3RecordsExactlyOnceForEachTrigger(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	uploadCalls := 0
	h.uploadS3Backup = func(context.Context, portainer.S3BackupSettings) error {
		uploadCalls++
		return nil
	}
	settings := portainer.S3BackupSettings{}

	require.NoError(t, h.exportS3(context.Background(), settings, "manual"))
	require.NoError(t, h.exportS3(context.Background(), settings, "scheduled"))

	require.Equal(t, 2, uploadCalls)
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, "scheduled", attempts[0].Trigger)
	require.Equal(t, "manual", attempts[1].Trigger)
	require.Equal(t, "succeeded", attempts[0].Outcome)
	require.False(t, h.backupStatus.Failed)
}

func TestExportS3AppendFailureKeepsNewerMemoryStatusOverOlderHistory(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	oldCompleted := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	olderAttempt := portainer.BackupAttempt{ID: 1, Trigger: "scheduled", StartedAtUTC: oldCompleted.Add(-time.Minute), CompletedAtUTC: oldCompleted, Outcome: "failed", ErrorCategory: "upload", SafeErrorMessage: "old failure"}
	require.NoError(t, store.BackupHistory().Append(&olderAttempt))
	failingStore := &failingHistoryDataStore{DataStore: store, history: &appendFailReadOldHistory{BackupHistoryService: store.BackupHistory(), err: errors.New("history unavailable")}}
	h := newTestBackupHandler(t, failingStore)
	h.uploadS3Backup = func(context.Context, portainer.S3BackupSettings) error { return nil }

	require.NoError(t, h.exportS3(context.Background(), portainer.S3BackupSettings{}, "manual"))
	status := h.getS3BackupStatus()

	require.Equal(t, "succeeded", status.State)
	require.False(t, status.Failed)
	require.False(t, status.HistoryAvailable)
	require.Equal(t, "manual", status.LatestTrigger)
	require.NotEqual(t, oldCompleted.Format(time.RFC3339), status.TimestampUTC)
	require.Len(t, status.History, 1)
	require.Equal(t, "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", status.History[0].SafeErrorMessage)
}

func TestExportS3AppendFailureDoesNotChangeUploadResultOrLegacyStatus(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	attemptService := &failingHistoryService{err: errors.New("history unavailable")}
	h := newTestBackupHandler(t, &failingHistoryDataStore{DataStore: store, history: attemptService})
	h.uploadS3Backup = func(context.Context, portainer.S3BackupSettings) error { return nil }
	h.backupStatus = portainer.BackupStatus{Failed: true, TimestampUTC: "old-time"}

	err = h.exportS3(context.Background(), portainer.S3BackupSettings{}, "manual")

	require.NoError(t, err)
	require.False(t, h.backupStatus.Failed)
	require.NotEqual(t, "old-time", h.backupStatus.TimestampUTC)
	require.Equal(t, "succeeded", h.backupStatus.State)
	require.False(t, h.backupStatus.HistoryAvailable)
}

func TestExportS3PreCancelledRequestDoesNotAcquireFreeSlot(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	uploadCalls := 0
	h.uploadS3Backup = func(context.Context, portainer.S3BackupSettings) error {
		uploadCalls++
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = h.exportS3(ctx, portainer.S3BackupSettings{}, "manual")

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, uploadCalls)
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Empty(t, attempts)
	require.Nil(t, h.runningExport)
}

func TestExportS3CancelledWhileWaitingDoesNotStart(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	started := make(chan struct{})
	unblock := make(chan struct{})
	uploadCalls := 0
	h.uploadS3Backup = func(ctx context.Context, _ portainer.S3BackupSettings) error {
		uploadCalls++
		if uploadCalls == 1 {
			close(started)
			<-unblock
			return nil
		}
		return nil
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- h.exportS3(context.Background(), portainer.S3BackupSettings{}, "scheduled") }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = h.exportS3(ctx, portainer.S3BackupSettings{}, "manual")
	close(unblock)
	require.NoError(t, <-firstDone)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, uploadCalls)
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
}

func TestExportS3FailureUsesSafeErrorInHistory(t *testing.T) {
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	h := newTestBackupHandler(t, store)
	h.uploadS3Backup = func(context.Context, portainer.S3BackupSettings) error {
		return operations.NewS3ExportError("upload", "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", errors.New("upload failed with secret=do-not-store"))
	}

	err = h.exportS3(context.Background(), portainer.S3BackupSettings{}, "manual")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "do-not-store")
	attempts, err := store.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "upload", attempts[0].ErrorCategory)
	require.NotContains(t, attempts[0].SafeErrorMessage, "do-not-store")
}

func newTestBackupHandler(t *testing.T, store dataservices.DataStore) *Handler {
	t.Helper()
	return NewHandler(testhelpers.NewTestRequestBouncer(), store, nil, t.TempDir(), func() {}, adminmonitor.New(time.Hour, nil))
}

func statusRequest(isAdmin bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/backup/s3/status", nil)
	return r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{IsAdmin: isAdmin}))
}

type failingHistoryDataStore struct {
	dataservices.DataStore
	history dataservices.BackupHistoryService
}

func (s *failingHistoryDataStore) BackupHistory() dataservices.BackupHistoryService {
	return s.history
}

type failingHistoryService struct {
	dataservices.BackupHistoryService
	err error
}

func (s failingHistoryService) Append(*portainer.BackupAttempt) error { return s.err }

func (s failingHistoryService) ReadAll() ([]portainer.BackupAttempt, error) { return nil, s.err }

type appendFailReadOldHistory struct {
	dataservices.BackupHistoryService
	err error
}

func (s *appendFailReadOldHistory) Append(*portainer.BackupAttempt) error { return s.err }
