package backup

import (
	"context"
	"net/http"
	"time"

	portainer "github.com/portainer/portainer/api"
	operations "github.com/portainer/portainer/api/backup"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type s3BackupPayload portainer.S3BackupSettings

type s3BackupSettingsResponse struct {
	AccessKeyID      string `json:"accessKeyID"`
	Region           string `json:"region"`
	BucketName       string `json:"bucketName"`
	CronRule         string `json:"cronRule"`
	S3CompatibleHost string `json:"s3CompatibleHost"`
}

func (p *s3BackupPayload) Validate(r *http.Request) error {
	return nil
}

func (h *Handler) s3Settings(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	settings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve backup settings", err)
	}

	return response.JSON(w, redactS3BackupSettings(settings.S3BackupSettings))
}

func (h *Handler) saveS3Settings(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload s3BackupPayload
	if err := request.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return httperror.BadRequest("Invalid S3 backup settings", err)
	}

	settings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve backup settings", err)
	}

	newSettings := portainer.S3BackupSettings(payload)
	if newSettings.SecretAccessKey == "" {
		newSettings.SecretAccessKey = settings.S3BackupSettings.SecretAccessKey
	}
	if newSettings.Password == "" {
		newSettings.Password = settings.S3BackupSettings.Password
	}
	if err := h.reconcileS3Schedule(newSettings); err != nil {
		return httperror.BadRequest("Invalid S3 backup schedule", err)
	}

	settings.S3BackupSettings = newSettings
	if err := h.dataStore.Settings().UpdateSettings(settings); err != nil {
		return httperror.InternalServerError("Unable to save backup settings", err)
	}

	return response.JSON(w, redactS3BackupSettings(settings.S3BackupSettings))
}

func (h *Handler) exportS3Backup(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload s3BackupPayload
	if err := request.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return httperror.BadRequest("Invalid S3 backup settings", err)
	}

	settings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve backup settings", err)
	}

	backupSettings := portainer.S3BackupSettings(payload)
	if backupSettings.SecretAccessKey == "" {
		backupSettings.SecretAccessKey = settings.S3BackupSettings.SecretAccessKey
	}
	if backupSettings.Password == "" {
		backupSettings.Password = settings.S3BackupSettings.Password
	}
	if err := operations.ValidateS3BackupSettings(backupSettings); err != nil {
		return httperror.BadRequest("Invalid S3 backup settings", err)
	}

	if err := h.exportS3(context.Background(), backupSettings); err != nil {
		return httperror.InternalServerError("Unable to export backup to S3", err)
	}

	return nil
}

func (h *Handler) s3Status(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()

	return response.JSON(w, h.backupStatus)
}

func (h *Handler) exportS3(ctx context.Context, settings portainer.S3BackupSettings) error {
	h.exportMu.Lock()
	defer h.exportMu.Unlock()

	err := operations.UploadS3Backup(ctx, settings, h.gate, h.dataStore, h.filestorePath)

	h.statusMu.Lock()
	h.backupStatus = portainer.BackupStatus{
		Failed:       err != nil,
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	}
	h.statusMu.Unlock()

	return err
}

func (h *Handler) restoreS3Schedule() error {
	if h.scheduler == nil {
		return nil
	}

	settings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return err
	}

	return h.reconcileS3Schedule(settings.S3BackupSettings)
}

func (h *Handler) reconcileS3Schedule(settings portainer.S3BackupSettings) error {
	h.scheduleMu.Lock()
	defer h.scheduleMu.Unlock()

	if h.scheduler == nil {
		return nil
	}

	if settings.CronRule == "" {
		if h.scheduledJobID != "" {
			if err := h.scheduler.StopJob(h.scheduledJobID); err != nil {
				return err
			}
			h.scheduledJobID = ""
		}

		return nil
	}

	jobID, err := h.scheduler.StartJobCron(settings.CronRule, func() error {
		return h.exportS3(context.Background(), settings)
	})
	if err != nil {
		return err
	}
	previousJobID := h.scheduledJobID
	h.scheduledJobID = jobID
	if previousJobID != "" {
		if err := h.scheduler.StopJob(previousJobID); err != nil {
			return err
		}
	}

	return nil
}

func redactS3BackupSettings(settings portainer.S3BackupSettings) s3BackupSettingsResponse {
	return s3BackupSettingsResponse{
		AccessKeyID:      settings.AccessKeyID,
		Region:           settings.Region,
		BucketName:       settings.BucketName,
		CronRule:         settings.CronRule,
		S3CompatibleHost: settings.S3CompatibleHost,
	}
}
