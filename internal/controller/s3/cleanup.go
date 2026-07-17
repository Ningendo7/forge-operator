package s3storage

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func CleanupBucket(
	ctx context.Context,
	client *s3.Client,
	app *forgev1alpha1.MyApp,
) error {
	bucketName := fmt.Sprintf("%s-%s-bucket", app.Namespace, app.Name)

	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: &bucketName,
	})

	return err
}
