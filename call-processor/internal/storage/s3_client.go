package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Client interface for storage operations
type Client interface {
	UploadObject(ctx context.Context, input *UploadObjectInput) (*UploadObjectOutput, error)
	DownloadObject(ctx context.Context, input *DownloadObjectInput) (*DownloadObjectOutput, error)
	DeleteObject(ctx context.Context, input *DeleteObjectInput) error
	GetPresignedURL(ctx context.Context, input *GetPresignedURLInput) (string, error)
	ListObjects(ctx context.Context, input *ListObjectsInput) (*ListObjectsOutput, error)
}

// Config for S3 client
type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	UseTLS          bool
}

// S3Client implements Client using AWS S3
type S3Client struct {
	client *s3.Client
}

// NewS3Client creates a new S3 client
func NewS3Client(cfg Config) (*S3Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               cfg.Endpoint,
			SigningRegion:     cfg.Region,
			HostnameImmutable: true,
		}, nil
	})

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
	})

	return &S3Client{
		client: client,
	}, nil
}

// UploadObjectInput represents input for uploading an object
type UploadObjectInput struct {
	Bucket   string
	Key      string
	Body     io.Reader
	Size     int64
	Metadata map[string]string
}

// UploadObjectOutput represents the output from uploading an object
type UploadObjectOutput struct {
	Location  string
	VersionID *string
	ETag      string
}

// UploadObject uploads an object to S3
func (c *S3Client) UploadObject(ctx context.Context, input *UploadObjectInput) (*UploadObjectOutput, error) {
	putObjectInput := &s3.PutObjectInput{
		Bucket:   aws.String(input.Bucket),
		Key:      aws.String(input.Key),
		Body:     input.Body,
		Metadata: input.Metadata,
	}

	result, err := c.client.PutObject(ctx, putObjectInput)
	if err != nil {
		return nil, fmt.Errorf("failed to upload object: %w", err)
	}

	location := fmt.Sprintf("s3://%s/%s", input.Bucket, input.Key)

	return &UploadObjectOutput{
		Location:  location,
		VersionID: result.VersionId,
		ETag:      *result.ETag,
	}, nil
}

// DownloadObjectInput represents input for downloading an object
type DownloadObjectInput struct {
	Bucket string
	Key    string
}

// DownloadObjectOutput represents the output from downloading an object
type DownloadObjectOutput struct {
	Body     io.ReadCloser
	Metadata map[string]string
	Size     int64
}

// DownloadObject downloads an object from S3
func (c *S3Client) DownloadObject(ctx context.Context, input *DownloadObjectInput) (*DownloadObjectOutput, error) {
	getObjectInput := &s3.GetObjectInput{
		Bucket: aws.String(input.Bucket),
		Key:    aws.String(input.Key),
	}

	result, err := c.client.GetObject(ctx, getObjectInput)
	if err != nil {
		return nil, fmt.Errorf("failed to download object: %w", err)
	}

	return &DownloadObjectOutput{
		Body:     result.Body,
		Metadata: result.Metadata,
		Size:     *result.ContentLength,
	}, nil
}

// DeleteObjectInput represents input for deleting an object
type DeleteObjectInput struct {
	Bucket string
	Key    string
}

// DeleteObject deletes an object from S3
func (c *S3Client) DeleteObject(ctx context.Context, input *DeleteObjectInput) error {
	deleteObjectInput := &s3.DeleteObjectInput{
		Bucket: aws.String(input.Bucket),
		Key:    aws.String(input.Key),
	}

	_, err := c.client.DeleteObject(ctx, deleteObjectInput)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// GetPresignedURLInput represents input for generating a presigned URL
type GetPresignedURLInput struct {
	Bucket  string
	Key     string
	Expires time.Duration
}

// GetPresignedURL generates a presigned URL for an object
func (c *S3Client) GetPresignedURL(ctx context.Context, input *GetPresignedURLInput) (string, error) {
	presignClient := s3.NewPresignClient(c.client)

	getObjectInput := &s3.GetObjectInput{
		Bucket: aws.String(input.Bucket),
		Key:    aws.String(input.Key),
	}

	presignedURL, err := presignClient.PresignGetObject(ctx, getObjectInput, s3.WithPresignExpires(input.Expires))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedURL.URL, nil
}

// ListObjectsInput represents input for listing objects
type ListObjectsInput struct {
	Bucket    string
	Prefix    string
	Delimiter string
	MaxKeys   int32
	StartAfter string
}

// ListObjectsOutput represents the output from listing objects
type ListObjectsOutput struct {
	Contents       []ObjectInfo
	CommonPrefixes []string
	IsTruncated    bool
	NextContinuationToken string
}

// ObjectInfo represents information about an object
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// ListObjects lists objects in a bucket
func (c *S3Client) ListObjects(ctx context.Context, input *ListObjectsInput) (*ListObjectsOutput, error) {
	listObjectsInput := &s3.ListObjectsV2Input{
		Bucket:     aws.String(input.Bucket),
		Prefix:     aws.String(input.Prefix),
		Delimiter:  aws.String(input.Delimiter),
		MaxKeys:    aws.Int32(input.MaxKeys),
		StartAfter: aws.String(input.StartAfter),
	}

	result, err := c.client.ListObjectsV2(ctx, listObjectsInput)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	output := &ListObjectsOutput{
		Contents:    make([]ObjectInfo, 0, len(result.Contents)),
		CommonPrefixes: make([]string, 0, len(result.CommonPrefixes)),
		IsTruncated: result.IsTruncated,
	}

	if result.NextContinuationToken != nil {
		output.NextContinuationToken = *result.NextContinuationToken
	}

	for _, obj := range result.Contents {
		output.Contents = append(output.Contents, ObjectInfo{
			Key:          *obj.Key,
			Size:         obj.Size,
			LastModified: *obj.LastModified,
			ETag:         *obj.ETag,
		})
	}

	for _, prefix := range result.CommonPrefixes {
		output.CommonPrefixes = append(output.CommonPrefixes, *prefix.Prefix)
	}

	return output, nil
}