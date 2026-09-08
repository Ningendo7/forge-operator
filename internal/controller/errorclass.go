package controller

import (
	"context"
	"errors"
	"net/http"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	smithy "github.com/aws/smithy-go"
	"github.com/linode/linodego"

	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
)

// Outcome label values recorded against forge_storage_reconcile_total and
// forge_finalizer_cleanup_total (see internal/controller/observability). Kept as
// a small, fixed set rather than raw error text -- see that package's doc
// comment for why.
const (
	outcomeReady        = "ready"
	outcomeSuccess      = "success"
	outcomeNotOwned     = "not_owned"
	outcomeTimeout      = "timeout"
	outcomeAccessDenied = "access_denied"
	outcomeOther        = "other_error"
)

// classifyAWSStorageError maps a non-nil error returned from the s3
// package's ReconcileBucket/CleanupBucket into one of the outcome*
// constants above. notOwnedOutcome lets callers choose the label used for
// ErrBucketNotOwned (outcomeNotOwned during reconcile) or opt out of it
// entirely (pass "" during cleanup, which never re-verifies ownership, so
// that check is skipped rather than silently never matching).
func classifyAWSStorageError(err error, notOwnedOutcome string) string {
	switch {
	case notOwnedOutcome != "" && errors.Is(err, s3storage.ErrBucketNotOwned):
		return outcomeNotOwned
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	case isAWSAccessDenied(err):
		return outcomeAccessDenied
	default:
		return outcomeOther
	}
}

// isAWSAccessDenied checks both of the two shapes an AWS permissions
// failure actually arrives in across this codebase: a transport-level HTTP
// 403 (returned by S3's HeadBucket, see desireds3.go's own status-code
// switch) and a modeled API exception with an AccessDenied-family error
// code (returned by IAM's typed exceptions in ReconcileAppIRSA). Both
// survive errors.As through the %w-wrapped chain these errors travel
// through on their way up to storage.go/finalizer.go.
func isAWSAccessDenied(err error) bool {
	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusForbidden {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "AccessDenied", "AccessDeniedException", "UnauthorizedException":
			return true
		}
	}
	return false
}

// classifyAkamaiStorageError is classifyAWSStorageError's Akamai
// equivalent. linodego surfaces HTTP status directly on *linodego.Error,
// unlike AWS's split between transport-level and modeled API errors, so
// this is simpler.
func classifyAkamaiStorageError(err error, notOwnedOutcome string) string {
	switch {
	case notOwnedOutcome != "" && errors.Is(err, akamaiobjstr.ErrBucketNotOwned):
		return outcomeNotOwned
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	case isAkamaiAccessDenied(err):
		return outcomeAccessDenied
	default:
		return outcomeOther
	}
}

func isAkamaiAccessDenied(err error) bool {
	var lerr *linodego.Error
	return errors.As(err, &lerr) && (lerr.Code == http.StatusForbidden || lerr.Code == http.StatusUnauthorized)
}
