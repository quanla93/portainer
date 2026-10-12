package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/logs"
	"github.com/rs/zerolog/log"
)

const S3ConnectionTestTimeout = 15 * time.Second

type S3ExportError struct {
	Category string
	message  string
	cause    error
}

func (e *S3ExportError) Error() string { return e.message }

func (e *S3ExportError) Unwrap() error { return e.cause }

func NewS3ExportError(category, message string, cause error) *S3ExportError {
	return &S3ExportError{Category: category, message: message, cause: cause}
}

type s3PutObjectClient interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type S3ConnectionTestResult struct {
	Success  bool   `json:"success"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// CheckS3BucketAccess verifies read-only access to the configured bucket.
func CheckS3BucketAccess(ctx context.Context, settings portainer.S3BackupSettings) S3ConnectionTestResult {
	if err := ValidateS3BackupSettings(settings); err != nil {
		return S3ConnectionTestResult{Category: "invalid_configuration", Message: "Backup settings are incomplete or invalid. Review the S3 backup configuration."}
	}

	probeCtx, cancel := context.WithTimeout(ctx, S3ConnectionTestTimeout)
	defer cancel()

	client, err := newS3Client(probeCtx, settings)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return S3ConnectionTestResult{Category: "timeout", Message: "The S3 operation timed out. Verify provider availability and network latency."}
		}
		if errors.Is(err, context.Canceled) || errors.Is(probeCtx.Err(), context.Canceled) {
			return S3ConnectionTestResult{Category: "network", Message: "The S3 access check was cancelled before completion."}
		}
		return S3ConnectionTestResult{Category: "bucket_access", Message: "The S3 bucket is unavailable or access was denied. Verify the bucket name and permissions."}
	}

	_, err = client.HeadBucket(probeCtx, &s3.HeadBucketInput{Bucket: aws.String(settings.BucketName)})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return S3ConnectionTestResult{Category: "timeout", Message: "The S3 operation timed out. Verify provider availability and network latency."}
		}
		if errors.Is(err, context.Canceled) || errors.Is(probeCtx.Err(), context.Canceled) {
			return S3ConnectionTestResult{Category: "network", Message: "The S3 access check was cancelled before completion."}
		}
		return S3ConnectionTestResult{Category: "bucket_access", Message: "The S3 bucket is unavailable or access was denied. Verify the bucket name and permissions."}
	}

	return S3ConnectionTestResult{Success: true, Category: "bucket_access_verified", Message: "Portainer can access the S3 bucket. Upload permission was not tested."}
}

var newS3Client = func(ctx context.Context, settings portainer.S3BackupSettings) (s3PutObjectClient, error) {
	region := settings.Region
	if region == "" {
		region = "us-east-1"
	}

	configOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(settings.AccessKeyID, settings.SecretAccessKey, "")),
	}
	if settings.S3CompatibleHost != "" {
		configOptions = append(configOptions, awsconfig.WithBaseEndpoint(settings.S3CompatibleHost))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, configOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}

	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		// S3-compatible services, including Cloudflare R2, require path-style requests.
		options.UsePathStyle = settings.S3CompatibleHost != ""
	}), nil
}

// UploadS3Backup creates a Portainer backup archive and writes it to an S3-compatible bucket.
func UploadS3Backup(ctx context.Context, settings portainer.S3BackupSettings, gate *offlinegate.OfflineGate, dataStore dataservices.DataStore, filestorePath string) error {
	if err := ValidateS3BackupSettings(settings); err != nil {
		return NewS3ExportError("invalid_configuration", "Backup settings are incomplete or invalid. Review the S3 backup configuration.", err)
	}

	archivePath, err := CreateBackupArchive(settings.Password, gate, dataStore, filestorePath)
	if err != nil {
		return NewS3ExportError("archive", "Portainer could not create the backup archive. Review server storage and protected logs.", err)
	}
	defer func() {
		if err := os.RemoveAll(filepath.Dir(archivePath)); err != nil {
			log.Warn().Err(err).Msg("failed to remove backup temp folder")
		}
	}()

	archive, err := os.Open(archivePath)
	if err != nil {
		return NewS3ExportError("archive", "Portainer could not open the backup archive. Review server storage and protected logs.", err)
	}
	defer logs.CloseAndLogErr(archive)

	client, err := newS3Client(ctx, settings)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return NewS3ExportError("timeout", "The S3 backup operation timed out. Verify provider availability and network latency.", err)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return NewS3ExportError("network", "The S3 backup operation was cancelled before completion.", err)
		}
		return NewS3ExportError("bucket_access", "The S3 provider could not be reached or denied access. Verify endpoint and bucket permissions.", err)
	}

	objectKey := "portainer-backup_" + filepath.Base(archivePath)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(settings.BucketName),
		Key:    aws.String(objectKey),
		Body:   io.Reader(archive),
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return NewS3ExportError("timeout", "The S3 backup upload timed out. Verify provider availability and network latency.", err)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return NewS3ExportError("network", "The S3 backup upload was cancelled before completion.", err)
		}
		return NewS3ExportError("upload", "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", err)
	}

	return nil
}

func ValidateS3BackupSettings(settings portainer.S3BackupSettings) error {
	if settings.AccessKeyID == "" {
		return fmt.Errorf("S3 access key ID is required")
	}
	if settings.SecretAccessKey == "" {
		return fmt.Errorf("S3 secret access key is required")
	}
	if settings.BucketName == "" {
		return fmt.Errorf("S3 bucket name is required")
	}

	return nil
}
