package backuphistory

import (
	"fmt"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
)

type ServiceTx struct {
	dataservices.BaseDataServiceTx[portainer.BackupAttempt, int]
}

func (s ServiceTx) Append(attempt *portainer.BackupAttempt) error {
	if err := validateAttempt(attempt); err != nil {
		return err
	}
	id, err := s.Tx.SetNextIdentifier(BucketName)
	if err != nil {
		return fmt.Errorf("allocate backup history identifier: %w", err)
	}
	stored := *attempt
	stored.ID = id
	stored.StartedAtUTC = stored.StartedAtUTC.UTC()
	stored.CompletedAtUTC = stored.CompletedAtUTC.UTC()
	if stored.Outcome == OutcomeFailed {
		stored.SafeErrorMessage = safeMessageForCategory(stored.ErrorCategory)
	}
	if err := s.Tx.CreateObjectWithId(BucketName, id, &stored); err != nil {
		return fmt.Errorf("store backup history attempt: %w", err)
	}
	return s.prune()
}

func (s ServiceTx) ReadAll() ([]portainer.BackupAttempt, error) {
	attempts, err := s.BaseDataServiceTx.ReadAll()
	if err != nil {
		return nil, err
	}
	sortAttempts(attempts)
	return attempts, nil
}

func (s ServiceTx) prune() error {
	attempts, err := s.ReadAll()
	if err != nil {
		return err
	}
	if len(attempts) <= MaxRecords {
		return nil
	}
	for _, attempt := range attempts[MaxRecords:] {
		if err := s.Tx.DeleteObject(BucketName, s.Connection.ConvertToKey(attempt.ID)); err != nil {
			return fmt.Errorf("prune backup history attempt: %w", err)
		}
	}
	return nil
}
