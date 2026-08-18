// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package blob

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Store struct {
	client *s3.Client
	bucket string
}

func newS3Store(cfg Config) (*s3Store, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("storage.s3.bucket required for the s3 backend")
	}

	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}

	opts := func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}

		o.Region = region
		o.UsePathStyle = cfg.S3UsePathStyle
	}

	if cfg.S3AccessKey != "" {
		// Static keys, which is what this backend supported and all it
		// supported. Kept as the explicit override.
		return &s3Store{
			client: s3.New(s3.Options{
				Credentials: credentials.NewStaticCredentialsProvider(
					cfg.S3AccessKey, cfg.S3SecretKey, ""),
			}, opts),
			bucket: cfg.S3Bucket,
		}, nil
	}

	// No keys configured: the DEFAULT CREDENTIAL CHAIN - environment,
	// shared config, an EC2 instance role over IMDS, an ECS task role, an
	// EKS service account.
	//
	// This was `s3.New(s3.Options{})` with Credentials left nil, which
	// resolves nothing at all: an installation on EC2 with a role that
	// grants the bucket could not use it, and the failure arrived on the
	// first attachment upload rather than at boot. Nothing about the
	// static path changes.
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("storage.s3: no credentials (set storage.s3.access_key, "+
			"or give this machine a role): %w", err)
	}

	return &s3Store{client: s3.NewFromConfig(awsCfg, opts), bucket: cfg.S3Bucket}, nil
}

// Put inserts the blob, or updates the row when its id already exists.
func (s *s3Store) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}

	return nil
}

// Get returns one blob by id, or nil when there is no such row.
func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %q: %w", key, err)
	}

	return out.Body, nil
}

// Delete removes one blob by id.
func (s *s3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete %q: %w", key, err)
	}

	return nil
}
