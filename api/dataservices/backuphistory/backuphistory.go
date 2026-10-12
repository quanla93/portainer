package backuphistory

import (
	"fmt"
	"sort"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
)

const (
	BucketName = "backup_history"
	MaxRecords = 20
)

const (
	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
)

// Attempt contains only the safe summary of a completed S3 export.
type Attempt = portainer.BackupAttempt

type Service struct {
	connection portainer.Connection
}

func NewService(connection portainer.Connection) (*Service, error) {
	if err := connection.SetServiceName(BucketName); err != nil {
		return nil, err
	}

	return &Service{connection: connection}, nil
}

// Append persists a completed attempt and prunes older records atomically.
func (s *Service) Append(attempt *portainer.BackupAttempt) error {
	if err := validateAttempt(attempt); err != nil {
		return err
	}

	return s.connection.UpdateTx(func(tx portainer.Transaction) error {
		return s.Tx(tx).Append(attempt)
	})
}

// ReadAll returns attempts ordered newest first, with ascending ID tie-breaks.
func (s *Service) ReadAll() ([]portainer.BackupAttempt, error) {
	var attempts []portainer.BackupAttempt
	err := s.connection.ViewTx(func(tx portainer.Transaction) error {
		var err error
		attempts, err = s.Tx(tx).ReadAll()
		return err
	})
	return attempts, err
}

func (s *Service) Tx(tx portainer.Transaction) ServiceTx {
	return ServiceTx{BaseDataServiceTx: dataservices.BaseDataServiceTx[portainer.BackupAttempt, int]{Bucket: BucketName, Connection: s.connection, Tx: tx}}
}

func validateAttempt(attempt *portainer.BackupAttempt) error {
	if attempt == nil {
		return fmt.Errorf("backup history attempt is required")
	}
	if attempt.Trigger != TriggerManual && attempt.Trigger != TriggerScheduled {
		return fmt.Errorf("invalid backup history trigger")
	}
	if attempt.Outcome != OutcomeSucceeded && attempt.Outcome != OutcomeFailed {
		return fmt.Errorf("invalid backup history outcome")
	}
	if attempt.StartedAtUTC.IsZero() || attempt.CompletedAtUTC.IsZero() || attempt.CompletedAtUTC.Before(attempt.StartedAtUTC) {
		return fmt.Errorf("invalid backup history timestamps")
	}
	if attempt.DurationMS < 0 {
		return fmt.Errorf("invalid backup history duration")
	}
	if attempt.Outcome == OutcomeSucceeded && (attempt.ErrorCategory != "" || attempt.SafeErrorMessage != "") {
		return fmt.Errorf("successful backup history attempt cannot contain error details")
	}
	if attempt.Outcome == OutcomeFailed && !isAllowedErrorCategory(attempt.ErrorCategory) {
		return fmt.Errorf("invalid backup history error category")
	}
	return nil
}

func safeMessageForCategory(category string) string {
	switch category {
	case "invalid_configuration":
		return "Backup settings are incomplete or invalid. Review the S3 backup configuration."
	case "authentication":
		return "The S3 provider rejected the credentials. Verify the access key and secret."
	case "bucket_access":
		return "The S3 bucket is unavailable or access was denied. Verify the bucket name and permissions."
	case "network":
		return "Portainer could not reach the S3 provider. Verify the endpoint and network connection."
	case "timeout":
		return "The S3 operation timed out. Verify provider availability and network latency."
	case "archive":
		return "Portainer could not create the backup archive. Review server storage and logs."
	case "upload":
		return "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability."
	default:
		return "The backup failed. Review the protected server logs for details."
	}
}

func isAllowedErrorCategory(category string) bool {
	switch category {
	case "invalid_configuration", "authentication", "bucket_access", "network", "timeout", "archive", "upload", "internal":
		return true
	default:
		return false
	}
}

func sortAttempts(attempts []portainer.BackupAttempt) {
	sort.Slice(attempts, func(i, j int) bool {
		if attempts[i].CompletedAtUTC.Equal(attempts[j].CompletedAtUTC) {
			return attempts[i].ID > attempts[j].ID
		}
		return attempts[i].CompletedAtUTC.After(attempts[j].CompletedAtUTC)
	})
}
