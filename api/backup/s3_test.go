package backup

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
)

type fakeS3Client struct {
	headBucketInput *s3.HeadBucketInput
	headBucketErr   error
	headBucketCalls int
	putObjectCalls  int
	ctxDone         <-chan struct{}
	deadline        time.Time
}

func (c *fakeS3Client) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	c.headBucketCalls++
	c.headBucketInput = input
	c.ctxDone = ctx.Done()
	c.deadline, _ = ctx.Deadline()
	return &s3.HeadBucketOutput{}, c.headBucketErr
}

func (c *fakeS3Client) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	c.putObjectCalls++
	return &s3.PutObjectOutput{}, nil
}

func validS3Settings() portainer.S3BackupSettings {
	return portainer.S3BackupSettings{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		BucketName:      "backup-bucket",
	}
}

func TestCheckS3BucketAccessUsesReadOnlyHeadBucket(t *testing.T) {
	client := &fakeS3Client{}
	oldNewS3Client := newS3Client
	newS3Client = func(context.Context, portainer.S3BackupSettings) (s3PutObjectClient, error) {
		return client, nil
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	result := CheckS3BucketAccess(context.Background(), validS3Settings())

	require.True(t, result.Success)
	require.Equal(t, "bucket_access_verified", result.Category)
	require.Equal(t, "backup-bucket", *client.headBucketInput.Bucket)
	require.Equal(t, 1, client.headBucketCalls)
	require.Zero(t, client.putObjectCalls)
}

func TestCheckS3BucketAccessMapsClientConstructionDeadline(t *testing.T) {
	oldNewS3Client := newS3Client
	newS3Client = func(ctx context.Context, _ portainer.S3BackupSettings) (s3PutObjectClient, error) {
		return nil, fmt.Errorf("client setup interrupted: %w", context.DeadlineExceeded)
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	result := CheckS3BucketAccess(context.Background(), validS3Settings())

	require.False(t, result.Success)
	require.Equal(t, "timeout", result.Category)
}

func TestCheckS3BucketAccessMapsClientConstructionCancellation(t *testing.T) {
	oldNewS3Client := newS3Client
	newS3Client = func(ctx context.Context, _ portainer.S3BackupSettings) (s3PutObjectClient, error) {
		return nil, ctx.Err()
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := CheckS3BucketAccess(ctx, validS3Settings())

	require.False(t, result.Success)
	require.Equal(t, "network", result.Category)
}

func TestCheckS3BucketAccessValidatesBeforeCreatingClient(t *testing.T) {
	clientCreated := false
	oldNewS3Client := newS3Client
	newS3Client = func(context.Context, portainer.S3BackupSettings) (s3PutObjectClient, error) {
		clientCreated = true
		return &fakeS3Client{}, nil
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	result := CheckS3BucketAccess(context.Background(), portainer.S3BackupSettings{BucketName: "backup-bucket"})

	require.False(t, result.Success)
	require.Equal(t, "invalid_configuration", result.Category)
	require.False(t, clientCreated)
}

func TestCheckS3BucketAccessTimesOutAndCancelsProviderRequest(t *testing.T) {
	client := &fakeS3Client{}
	started := make(chan struct{})
	oldNewS3Client := newS3Client
	newS3Client = func(context.Context, portainer.S3BackupSettings) (s3PutObjectClient, error) {
		return &blockingHeadBucketClient{fakeS3Client: client, started: started}, nil
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	resultCh := make(chan S3ConnectionTestResult, 1)
	go func() { resultCh <- CheckS3BucketAccess(context.Background(), validS3Settings()) }()
	<-started
	deadline := clientDeadline(t, client)
	require.LessOrEqual(t, time.Until(deadline), S3ConnectionTestTimeout)

	result := <-resultCh
	require.False(t, result.Success)
	require.Equal(t, "timeout", result.Category)
	select {
	case <-client.ctxDone:
	default:
		t.Fatal("timed out provider request context was not cancelled")
	}
	require.Zero(t, client.putObjectCalls)
}

func TestCheckS3BucketAccessHonorsParentCancellationAndDeadline(t *testing.T) {
	client := &fakeS3Client{}
	started := make(chan struct{})
	oldNewS3Client := newS3Client
	newS3Client = func(context.Context, portainer.S3BackupSettings) (s3PutObjectClient, error) {
		return &blockingHeadBucketClient{fakeS3Client: client, started: started}, nil
	}
	t.Cleanup(func() { newS3Client = oldNewS3Client })

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan S3ConnectionTestResult, 1)
	go func() { resultCh <- CheckS3BucketAccess(ctx, validS3Settings()) }()
	<-started

	deadline := clientDeadline(t, client)
	require.LessOrEqual(t, time.Until(deadline), S3ConnectionTestTimeout)
	cancel()

	result := <-resultCh
	require.False(t, result.Success)
	require.Equal(t, "network", result.Category)
	select {
	case <-client.ctxDone:
	default:
		t.Fatal("provider request context was not cancelled")
	}
	require.Zero(t, client.putObjectCalls)
}

type blockingHeadBucketClient struct {
	*fakeS3Client
	started chan struct{}
}

func (c *blockingHeadBucketClient) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	c.headBucketCalls++
	c.headBucketInput = input
	c.ctxDone = ctx.Done()
	c.deadline, _ = ctx.Deadline()
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func clientDeadline(t *testing.T, client *fakeS3Client) time.Time {
	t.Helper()
	require.False(t, client.deadline.IsZero())
	return client.deadline
}

func TestSafeS3ExportErrorUsesFixedCategoryMessage(t *testing.T) {
	err := NewS3ExportError("upload", "The backup could not be uploaded to the S3 provider. Verify bucket permissions and provider availability.", errors.New("raw provider secret=do-not-expose"))
	require.Equal(t, "upload", err.Category)
	require.NotContains(t, err.Error(), "do-not-expose")
}

func TestCheckS3BucketAccessMapsProviderErrorsToSafeCategories(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		category string
	}{
		{name: "provider error", err: errors.New("sensitive provider response"), category: "bucket_access"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeS3Client{headBucketErr: tt.err}
			oldNewS3Client := newS3Client
			newS3Client = func(context.Context, portainer.S3BackupSettings) (s3PutObjectClient, error) {
				return client, nil
			}
			t.Cleanup(func() { newS3Client = oldNewS3Client })

			result := CheckS3BucketAccess(context.Background(), validS3Settings())
			require.False(t, result.Success)
			require.Equal(t, tt.category, result.Category)
			require.NotContains(t, result.Message, "sensitive provider response")
			require.Zero(t, client.putObjectCalls)
		})
	}
}
