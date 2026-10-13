package backup

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	portainer "github.com/portainer/portainer/api"
	operations "github.com/portainer/portainer/api/backup"
	"github.com/portainer/portainer/api/dataservices/backuphistory"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

type s3BackupPayload portainer.S3BackupSettings

type s3BackupSettingsResponse struct {
	AccessKeyID               string `json:"accessKeyID"`
	Region                    string `json:"region"`
	BucketName                string `json:"bucketName"`
	CronRule                  string `json:"cronRule"`
	S3CompatibleHost          string `json:"s3CompatibleHost"`
	SecretAccessKeyConfigured bool   `json:"secretAccessKeyConfigured"`
	PasswordConfigured        bool   `json:"passwordConfigured"`
}

func (p *s3BackupPayload) Validate(r *http.Request) error {
	return nil
}

type s3ConnectionTestPayload struct {
	AccessKeyID      string `json:"accessKeyID"`
	SecretAccessKey  string `json:"secretAccessKey"`
	Region           string `json:"region"`
	BucketName       string `json:"bucketName"`
	S3CompatibleHost string `json:"s3CompatibleHost"`
}

func (p *s3ConnectionTestPayload) Validate(*http.Request) error { return nil }

func (h *Handler) testS3BackupConnection(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload s3ConnectionTestPayload
	if err := request.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return httperror.BadRequest("Invalid S3 connection test settings", err)
	}

	storedSettings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve backup settings", err)
	}

	settings := portainer.S3BackupSettings{
		AccessKeyID:      payload.AccessKeyID,
		SecretAccessKey:  payload.SecretAccessKey,
		Region:           payload.Region,
		BucketName:       payload.BucketName,
		S3CompatibleHost: payload.S3CompatibleHost,
	}
	if settings.SecretAccessKey == "" {
		settings.SecretAccessKey = storedSettings.S3BackupSettings.SecretAccessKey
	}

	check := h.checkS3BucketAccess
	if check == nil {
		check = operations.CheckS3BucketAccess
	}
	result := check(r.Context(), settings)
	return response.JSON(w, result)
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
		return httperror.BadRequest("Invalid S3 backup settings", operations.NewS3ExportError("invalid_configuration", "Backup settings are incomplete or invalid. Review the S3 backup configuration.", err))
	}

	if err := h.exportS3(r.Context(), backupSettings, backuphistory.TriggerManual); err != nil {
		return httperror.InternalServerError("Unable to export backup to S3", safeS3ExportError(err))
	}

	return nil
}

func (h *Handler) s3Status(w http.ResponseWriter, _ *http.Request) *httperror.HandlerError {
	status := h.getS3BackupStatus()
	return response.JSON(w, status)
}

func calculateS3BackupSchedule(cronRule string, now time.Time, location *time.Location) portainer.BackupSchedule {
	if cronRule == "" {
		return portainer.BackupSchedule{State: "disabled", TimezoneState: "unavailable"}
	}

	schedule, err := cron.ParseStandard(cronRule)
	if err != nil {
		return portainer.BackupSchedule{Enabled: true, State: "invalid", TimezoneState: "unavailable"}
	}

	nextRun := schedule.Next(now.In(location))
	if nextRun.IsZero() {
		return portainer.BackupSchedule{Enabled: true, State: "unavailable", TimezoneState: "unavailable"}
	}

	result := portainer.BackupSchedule{Enabled: true, State: "enabled", NextRunUTC: nextRun.UTC().Format(time.RFC3339), TimezoneState: "unavailable"}
	if timezone := resolvableIANATimezone(location); timezone != "" {
		result.TimezoneState = "available"
		result.Timezone = timezone
	}
	return result
}

func resolvableIANATimezone(location *time.Location) string {
	if location == nil || location.String() == "Local" || strings.HasPrefix(location.String(), "+") || strings.HasPrefix(location.String(), "-") {
		return ""
	}
	resolved, err := time.LoadLocation(location.String())
	if err != nil || resolved.String() != location.String() {
		return ""
	}
	return location.String()
}

func (h *Handler) s3BackupSchedule() portainer.BackupSchedule {
	settings, err := h.dataStore.Settings().Settings()
	if err != nil {
		return portainer.BackupSchedule{State: "unavailable", TimezoneState: "unavailable"}
	}
	now := time.Now
	if h.now != nil {
		now = h.now
	}
	return calculateS3BackupSchedule(settings.S3BackupSettings.CronRule, now(), time.Local)
}

func (h *Handler) getS3BackupStatus() portainer.BackupStatus {
	schedule := h.s3BackupSchedule()
	h.statusMu.Lock()
	status := h.backupStatus
	running := h.runningExport
	historyIncomplete := h.historyIncomplete
	memoryStatusKnown := status.TimestampUTC != ""
	h.statusMu.Unlock()

	history, err := h.dataStore.BackupHistory().ReadAll()
	if err != nil {
		log.Warn().Err(err).Msg("unable to read S3 backup history")
		status.HistoryAvailable = false
		status.History = []portainer.BackupAttempt{}
		status.Schedule = schedule
		if running != nil {
			status.State = "running"
			status.RunningTrigger = running.Trigger
		} else if !memoryStatusKnown {
			status.State = "unavailable"
		}
		return status
	}

	status.HistoryAvailable = true
	status.History = history
	status.Schedule = schedule
	if running != nil {
		status.State = "running"
		status.RunningTrigger = running.Trigger
		return status
	}

	if historyIncomplete && memoryStatusKnown {
		// The latest completed operation is known in this process but was not persisted.
		status.HistoryAvailable = false
		return status
	}

	status.RunningTrigger = ""
	if len(history) > 0 {
		latest := history[0]
		status.Failed = latest.Outcome == backuphistory.OutcomeFailed
		status.TimestampUTC = latest.CompletedAtUTC.Format(time.RFC3339)
		status.State = latest.Outcome
		status.LatestTrigger = latest.Trigger
		status.LatestErrorCategory = latest.ErrorCategory
		status.LatestErrorMessage = latest.SafeErrorMessage
		return status
	}

	if memoryStatusKnown {
		if status.Failed {
			status.State = "failed"
		} else {
			status.State = "succeeded"
		}
		if historyIncomplete {
			status.HistoryAvailable = false
		}
		return status
	}

	status.Failed = false
	status.TimestampUTC = ""
	status.State = "neverRun"
	status.LatestTrigger = ""
	status.LatestErrorCategory = ""
	status.LatestErrorMessage = ""
	return status
}

func (h *Handler) runScheduledExport(settings portainer.S3BackupSettings) error {
	ctx := h.shutdownCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return h.exportS3(ctx, settings, backuphistory.TriggerScheduled)
}

func (h *Handler) exportS3(ctx context.Context, settings portainer.S3BackupSettings, trigger string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case h.exportMu <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-h.exportMu
			return err
		}
		defer func() { <-h.exportMu }()
	case <-ctx.Done():
		return ctx.Err()
	}

	startedAt := time.Now().UTC()
	running := &portainer.BackupAttempt{Trigger: trigger, StartedAtUTC: startedAt}
	h.statusMu.Lock()
	h.runningExport = running
	h.statusMu.Unlock()

	upload := h.uploadS3Backup
	if upload == nil {
		upload = func(ctx context.Context, settings portainer.S3BackupSettings) error {
			return operations.UploadS3Backup(ctx, settings, h.gate, h.dataStore, h.filestorePath)
		}
	}
	uploadErr := upload(ctx, settings)
	completedAt := time.Now().UTC()

	category, safeMessage := safeS3ExportFailure(uploadErr)
	attempt := &portainer.BackupAttempt{
		Trigger:        trigger,
		StartedAtUTC:   startedAt,
		CompletedAtUTC: completedAt,
		DurationMS:     completedAt.Sub(startedAt).Milliseconds(),
		Outcome:        backuphistory.OutcomeSucceeded,
	}
	if uploadErr != nil {
		attempt.Outcome = backuphistory.OutcomeFailed
		attempt.ErrorCategory = category
		attempt.SafeErrorMessage = safeMessage
	}
	historyErr := h.dataStore.BackupHistory().Append(attempt)
	if historyErr != nil {
		log.Warn().Err(historyErr).Str("trigger", trigger).Msg("unable to persist S3 backup history")
	}

	h.statusMu.Lock()
	h.runningExport = nil
	h.historyIncomplete = historyErr != nil
	h.backupStatus = portainer.BackupStatus{
		Failed:              uploadErr != nil,
		TimestampUTC:        completedAt.Format(time.RFC3339),
		State:               attempt.Outcome,
		LatestTrigger:       trigger,
		LatestErrorCategory: attempt.ErrorCategory,
		LatestErrorMessage:  attempt.SafeErrorMessage,
		HistoryAvailable:    historyErr == nil,
	}
	h.statusMu.Unlock()

	return uploadErr
}

func safeS3ExportFailure(err error) (string, string) {
	if err == nil {
		return "", ""
	}

	var exportErr *operations.S3ExportError
	if errors.As(err, &exportErr) {
		return exportErr.Category, exportErr.Error()
	}

	return "internal", "The backup failed. Review the protected server logs for details."
}

func safeS3ExportError(err error) error {
	if err == nil {
		return nil
	}
	_, message := safeS3ExportFailure(err)
	return errors.New(message)
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
		return h.runScheduledExport(settings)
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
		AccessKeyID:               settings.AccessKeyID,
		Region:                    settings.Region,
		BucketName:                settings.BucketName,
		CronRule:                  settings.CronRule,
		S3CompatibleHost:          settings.S3CompatibleHost,
		SecretAccessKeyConfigured: settings.SecretAccessKey != "",
		PasswordConfigured:        settings.Password != "",
	}
}
