package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/linode/linodego"

	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
)

// forbiddenResponseError builds the transport-level HTTP 403 shape
// isAWSAccessDenied checks for -- the same shape S3's HeadBucket returns,
// per desireds3.go's own status-code switch.
func forbiddenResponseError() error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusForbidden}},
		},
	}
}

func TestClassifyAWSStorageError(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		notOwnedOutcome string
		want            string
	}{
		{
			name:            "not owned, checked",
			err:             fmt.Errorf("wrap: %w", s3storage.ErrBucketNotOwned),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeNotOwned,
		},
		{
			// Cleanup never re-verifies ownership, so it passes "" to opt out
			// of this check entirely -- proving the opt-out actually works,
			// not just that ErrBucketNotOwned never realistically reaches
			// cleanup in practice.
			name:            "not owned, but caller opted out (cleanup path)",
			err:             fmt.Errorf("wrap: %w", s3storage.ErrBucketNotOwned),
			notOwnedOutcome: "",
			want:            outcomeOther,
		},
		{
			name:            "timeout",
			err:             fmt.Errorf("wrap: %w", context.DeadlineExceeded),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeTimeout,
		},
		{
			name:            "access denied via transport-level HTTP 403 (S3 HeadBucket shape)",
			err:             fmt.Errorf("wrap: %w", forbiddenResponseError()),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "access denied via modeled AccessDenied API error (IAM shape)",
			err:             fmt.Errorf("wrap: %w", &smithy.GenericAPIError{Code: "AccessDenied"}),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "access denied via modeled AccessDeniedException",
			err:             &smithy.GenericAPIError{Code: "AccessDeniedException"},
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "access denied via modeled UnauthorizedException",
			err:             &smithy.GenericAPIError{Code: "UnauthorizedException"},
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "unrelated modeled API error falls through to other",
			err:             &smithy.GenericAPIError{Code: "InternalError"},
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeOther,
		},
		{
			name:            "plain unrecognized error falls through to other",
			err:             errors.New("boom"),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeOther,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAWSStorageError(tt.err, tt.notOwnedOutcome); got != tt.want {
				t.Errorf("classifyAWSStorageError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyAkamaiStorageError(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		notOwnedOutcome string
		want            string
	}{
		{
			name:            "not owned, checked",
			err:             fmt.Errorf("wrap: %w", akamaiobjstr.ErrBucketNotOwned),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeNotOwned,
		},
		{
			name:            "not owned, but caller opted out (cleanup path)",
			err:             fmt.Errorf("wrap: %w", akamaiobjstr.ErrBucketNotOwned),
			notOwnedOutcome: "",
			want:            outcomeOther,
		},
		{
			name:            "timeout",
			err:             fmt.Errorf("wrap: %w", context.DeadlineExceeded),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeTimeout,
		},
		{
			name:            "access denied via 403",
			err:             fmt.Errorf("wrap: %w", &linodego.Error{Code: http.StatusForbidden}),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "access denied via 401",
			err:             &linodego.Error{Code: http.StatusUnauthorized},
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeAccessDenied,
		},
		{
			name:            "not found is not access denied",
			err:             &linodego.Error{Code: http.StatusNotFound},
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeOther,
		},
		{
			name:            "plain unrecognized error falls through to other",
			err:             errors.New("boom"),
			notOwnedOutcome: outcomeNotOwned,
			want:            outcomeOther,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAkamaiStorageError(tt.err, tt.notOwnedOutcome); got != tt.want {
				t.Errorf("classifyAkamaiStorageError() = %q, want %q", got, tt.want)
			}
		})
	}
}
