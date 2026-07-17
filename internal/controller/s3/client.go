package s3storage

import (
	"context"
	"fmt"
	
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

)

func NewS3Client(
	ctx context.Context,
	region string,
) (*s3.Client, error) {

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))

	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	return client, nil
}