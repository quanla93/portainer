package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	portainer "github.com/portainer/portainer/api"
	operations "github.com/portainer/portainer/api/backup"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/stretchr/testify/require"
)

type s3ConnectionTestResponse struct {
	Success  bool   `json:"success"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

func TestS3ConnectionTestRequiresAdmin(t *testing.T) {
	datastore := testhelpers.NewDatastore(testhelpers.WithSettingsService(&portainer.Settings{}))
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), datastore, nil, "", nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/backup/s3/test", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(security.StoreRestrictedRequestContext(request, &security.RestrictedRequestContext{IsAdmin: false}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestS3ConnectionTestDoesNotExposeProviderError(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("secret=provider-secret-value"))
	}))
	defer provider.Close()
	settings := &portainer.Settings{S3BackupSettings: portainer.S3BackupSettings{
		SecretAccessKey:  "saved-secret-value",
		S3CompatibleHost: provider.URL,
	}}
	datastore := testhelpers.NewDatastore(testhelpers.WithSettingsService(settings))
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), datastore, nil, "", nil, nil)
	body := `{"accessKeyID":"submitted-access-key","secretAccessKey":"","region":"us-east-1","bucketName":"backup-bucket","s3CompatibleHost":"` + provider.URL + `"}`
	request := httptest.NewRequest(http.MethodPost, "/backup/s3/test", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(security.StoreRestrictedRequestContext(request, &security.RestrictedRequestContext{IsAdmin: true}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "provider-secret-value")
	require.NotContains(t, response.Body.String(), "saved-secret-value")
	require.Contains(t, response.Body.String(), "bucket_access")
}

func TestS3ConnectionTestUsesSavedSecretAndDoesNotMutateSettings(t *testing.T) {
	originalSettings := &portainer.Settings{
		S3BackupSettings: portainer.S3BackupSettings{
			SecretAccessKey:  "saved-secret-value",
			Password:         "backup-password-not-used",
			S3CompatibleHost: "https://s3.example.invalid",
		},
	}
	settingsSnapshot := *originalSettings
	_, datastore, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	t.Cleanup(teardown)
	require.NoError(t, datastore.Settings().UpdateSettings(originalSettings))
	beforeHistory, err := datastore.BackupHistory().ReadAll()
	require.NoError(t, err)
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), datastore, nil, "", nil, nil)

	var receivedSettings portainer.S3BackupSettings
	handler.checkS3BucketAccess = func(_ context.Context, settings portainer.S3BackupSettings) operations.S3ConnectionTestResult {
		receivedSettings = settings
		return operations.S3ConnectionTestResult{Success: true, Category: "bucket_access_verified", Message: "Bucket access verified."}
	}
	body := `{"accessKeyID":"submitted-access-key","secretAccessKey":"","region":"us-east-1","bucketName":"backup-bucket","s3CompatibleHost":"https://s3.example.invalid"}`
	request := httptest.NewRequest(http.MethodPost, "/backup/s3/test", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(security.StoreRestrictedRequestContext(request, &security.RestrictedRequestContext{IsAdmin: true}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result s3ConnectionTestResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	require.True(t, result.Success, result.Message)
	require.Equal(t, "bucket_access_verified", result.Category)
	require.Equal(t, "saved-secret-value", receivedSettings.SecretAccessKey)
	require.Equal(t, "submitted-access-key", receivedSettings.AccessKeyID)
	require.Equal(t, "backup-bucket", receivedSettings.BucketName)
	require.NotContains(t, response.Body.String(), "saved-secret-value")
	require.NotContains(t, response.Body.String(), "backup-password-not-used")
	settings, err := datastore.Settings().Settings()
	require.NoError(t, err)
	require.Equal(t, settingsSnapshot, *settings)
	afterHistory, err := datastore.BackupHistory().ReadAll()
	require.NoError(t, err)
	require.Equal(t, beforeHistory, afterHistory)
}
