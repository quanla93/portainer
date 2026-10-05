package backup

import (
	"context"
	"net/http"
	"sync"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/adminmonitor"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/scheduler"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// Handler is an http handler responsible for backup and restore portainer state
type Handler struct {
	*mux.Router
	bouncer         security.BouncerService
	dataStore       dataservices.DataStore
	gate            *offlinegate.OfflineGate
	filestorePath   string
	shutdownTrigger context.CancelFunc
	adminMonitor    *adminmonitor.Monitor
	scheduler       *scheduler.Scheduler
	SetupToken      string
	exportMu        sync.Mutex
	scheduleMu      sync.Mutex
	scheduledJobID  string
	statusMu        sync.Mutex
	backupStatus    portainer.BackupStatus
}

// NewHandler creates an new instance of backup handler
func NewHandler(
	bouncer security.BouncerService,
	dataStore dataservices.DataStore,
	gate *offlinegate.OfflineGate,
	filestorePath string,
	shutdownTrigger context.CancelFunc,
	adminMonitor *adminmonitor.Monitor,
	schedulers ...*scheduler.Scheduler,
) *Handler {
	var backupScheduler *scheduler.Scheduler
	if len(schedulers) > 0 {
		backupScheduler = schedulers[0]
	}
	h := &Handler{
		Router:          mux.NewRouter(),
		bouncer:         bouncer,
		dataStore:       dataStore,
		gate:            gate,
		filestorePath:   filestorePath,
		shutdownTrigger: shutdownTrigger,
		adminMonitor:    adminMonitor,
		scheduler:       backupScheduler,
	}

	h.Handle("/backup", bouncer.RestrictedAccess(adminAccess(httperror.LoggerHandler(h.backup)))).Methods(http.MethodPost)
	h.Handle("/backup/s3/settings", bouncer.RestrictedAccess(adminAccess(httperror.LoggerHandler(h.s3Settings)))).Methods(http.MethodGet)
	h.Handle("/backup/s3/settings", bouncer.RestrictedAccess(adminAccess(httperror.LoggerHandler(h.saveS3Settings)))).Methods(http.MethodPost)
	h.Handle("/backup/s3/execute", bouncer.RestrictedAccess(adminAccess(httperror.LoggerHandler(h.exportS3Backup)))).Methods(http.MethodPost)
	h.Handle("/backup/s3/status", bouncer.RestrictedAccess(adminAccess(httperror.LoggerHandler(h.s3Status)))).Methods(http.MethodGet)
	h.Handle("/restore", bouncer.PublicAccess(httperror.LoggerHandler(h.restore))).Methods(http.MethodPost)

	if err := h.restoreS3Schedule(); err != nil {
		log.Warn().Err(err).Msg("failed to restore S3 backup schedule")
	}

	return h
}

func adminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityContext, err := security.RetrieveRestrictedRequestContext(r)
		if err != nil {
			httperror.WriteError(w, http.StatusInternalServerError, "Unable to retrieve user info from request context", err)
			return
		}

		if !securityContext.IsAdmin {
			httperror.WriteError(w, http.StatusUnauthorized, "User is not authorized to perform the action", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}
