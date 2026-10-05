package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

type s3PutObjectClient interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
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
		return err
	}

	archivePath, err := CreateBackupArchive(settings.Password, gate, dataStore, filestorePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(filepath.Dir(archivePath)); err != nil {
			log.Warn().Err(err).Msg("failed to remove backup temp folder")
		}
	}()

	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer logs.CloseAndLogErr(archive)

	client, err := newS3Client(ctx, settings)
	if err != nil {
		return err
	}

	objectKey := "portainer-backup_" + filepath.Base(archivePath)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(settings.BucketName),
		Key:    aws.String(objectKey),
		Body:   io.Reader(archive),
	})
	if err != nil {
		return fmt.Errorf("upload backup to S3: %w", err)
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
