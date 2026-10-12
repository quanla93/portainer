package backuphistory_test

import (
	"errors"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices/backuphistory"
	"github.com/portainer/portainer/api/datastore"
	"github.com/stretchr/testify/require"
)

func TestAppendReadAndRetainLatestTwenty(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	service := ds.BackupHistory()
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.FixedZone("offset", 3600))

	for i := 0; i < 25; i++ {
		attempt := portainer.BackupAttempt{
			Trigger:        "manual",
			StartedAtUTC:   base.Add(time.Duration(i) * time.Minute),
			CompletedAtUTC: base.Add(time.Duration(i) * time.Minute),
			DurationMS:     int64(i),
			Outcome:        "succeeded",
		}
		require.NoError(t, service.Append(&attempt))
	}

	attempts, err := service.ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 20)
	require.Equal(t, 25, attempts[0].ID)
	require.Equal(t, 6, attempts[19].ID)
	require.Equal(t, time.UTC, attempts[0].CompletedAtUTC.Location())
}

func TestAppendOrdersEqualCompletionTimesByID(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	service := ds.BackupHistory()
	completed := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		require.NoError(t, service.Append(&portainer.BackupAttempt{
			Trigger: "scheduled", StartedAtUTC: completed, CompletedAtUTC: completed, Outcome: "succeeded",
		}))
	}
	attempts, err := service.ReadAll()
	require.NoError(t, err)
	require.Equal(t, []int{3, 2, 1}, []int{attempts[0].ID, attempts[1].ID, attempts[2].ID})
}

func TestAppendRejectsInvalidOrUnboundedInput(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	service := ds.BackupHistory()
	now := time.Now().UTC()
	for _, attempt := range []*portainer.BackupAttempt{
		nil,
		{Trigger: "test", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "succeeded"},
		{Trigger: "manual", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "pending"},
		{Trigger: "manual", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "failed", ErrorCategory: "provider_raw_error"},
	} {
		require.Error(t, service.Append(attempt))
	}
	attempts, err := service.ReadAll()
	require.NoError(t, err)
	require.Empty(t, attempts)
}

func TestAppendReplacesCallerErrorWithSafeCategoryMessage(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	now := time.Now().UTC()
	require.NoError(t, ds.BackupHistory().Append(&portainer.BackupAttempt{
		Trigger: "manual", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "failed", ErrorCategory: "upload", SafeErrorMessage: "secret=do-not-store token=also-secret",
	}))
	attempts, err := ds.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.NotContains(t, attempts[0].SafeErrorMessage, "do-not-store")
	require.NotContains(t, attempts[0].SafeErrorMessage, "also-secret")
	require.Equal(t, "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", attempts[0].SafeErrorMessage)
}

func TestAppendRetainsNewestIDsForEqualCompletionTimes(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	service := ds.BackupHistory()
	completed := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		require.NoError(t, service.Append(&portainer.BackupAttempt{
			Trigger: "scheduled", StartedAtUTC: completed, CompletedAtUTC: completed, Outcome: "succeeded",
		}))
	}
	attempts, err := service.ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 20)
	require.Equal(t, 25, attempts[0].ID)
	require.Equal(t, 6, attempts[len(attempts)-1].ID)
}

func TestAppendPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	now := time.Now().UTC()
	require.NoError(t, ds.BackupHistory().Append(&portainer.BackupAttempt{
		Trigger: "manual", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "failed", ErrorCategory: "upload", SafeErrorMessage: "Upload failed",
	}))
	require.NoError(t, ds.Close())
	_, err := ds.Open()
	require.NoError(t, err)
	attempts, err := ds.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", attempts[0].SafeErrorMessage)
}

func TestAppendTransactionRollback(t *testing.T) {
	t.Parallel()
	_, ds := datastore.MustNewTestStore(t, false, false)
	now := time.Now().UTC()
	rollbackErr := errors.New("rollback")
	history, err := backuphistory.NewService(ds.Connection())
	require.NoError(t, err)
	err = ds.Connection().UpdateTx(func(tx portainer.Transaction) error {
		if err := history.Tx(tx).Append(&portainer.BackupAttempt{
			Trigger: "manual", StartedAtUTC: now, CompletedAtUTC: now, Outcome: "succeeded",
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	require.ErrorIs(t, err, rollbackErr)
	attempts, err := ds.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Empty(t, attempts)
}
