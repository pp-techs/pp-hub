package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/zeebo/blake3"
)

// S3Store keeps blobs in an S3-compatible bucket under the same
// <2-hex>/<hash> layout LocalStore uses on disk.
//
// Keeping the layout identical is deliberate: migrating an existing install is
// `aws s3 sync data/blobs s3://bucket/` with no key translation, and an
// operator can read either backend with the same mental model.
type S3Store struct {
	cli    *s3.Client
	bucket string
	// spool is where Put buffers while hashing; "" means the OS temp dir.
	spool string
}

// S3Config configures an S3-compatible backend. Endpoint and PathStyle exist
// for MinIO and friends; leave them empty for real AWS.
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	PathStyle bool
	Spool     string
}

func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket is required")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1" // MinIO ignores it, the SDK insists on one
	}

	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if cfg.AccessKey != "" {
		// Explicit credentials win; otherwise fall back to the default chain
		// (env, shared config, IRSA/instance role) so a real AWS deployment
		// need not put long-lived secrets in the environment.
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// MinIO and most self-hosted gateways do not do virtual-host style.
		o.UsePathStyle = cfg.PathStyle
	})
	return &S3Store{cli: cli, bucket: cfg.Bucket, spool: cfg.Spool}, nil
}

func (s *S3Store) key(hash string) string { return hash[:2] + "/" + hash }

// Put spools the reader to a temp file while hashing, then uploads under the
// content address.
//
// The spool is not avoidable: the object key *is* the hash, so it is unknown
// until the last byte has been read, and S3 needs a seekable body to sign and
// retry a request. This mirrors LocalStore, which also writes to a temp file
// before renaming into place, so both backends fail the same way when a disk
// fills mid-upload.
func (s *S3Store) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(s.spool, "pp-blob-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	h := blake3.New()
	size, err := io.Copy(tmp, io.TeeReader(r, h))
	if err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(h.Sum(nil))

	// Dedup: identical content is already at this address, and re-uploading
	// would only cost bandwidth.
	if exists, err := s.exists(ctx, hash); err != nil {
		return "", 0, err
	} else if exists {
		return hash, size, nil
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	_, err = s.cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.key(hash)),
		Body:          tmp,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return "", 0, fmt.Errorf("s3: put %s: %w", hash, err)
	}
	return hash, size, nil
}

func (s *S3Store) exists(ctx context.Context, hash string) (bool, error) {
	_, err := s.cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(hash)),
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("s3: head %s: %w", hash, err)
}

func (s *S3Store) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	if len(hash) < 2 {
		return nil, ErrNotExist
	}
	out, err := s.cli.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(hash)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotExist
		}
		return nil, fmt.Errorf("s3: get %s: %w", hash, err)
	}
	return out.Body, nil
}

// Delete removes a blob. Missing is not an error: GC re-checks ref_count and
// may race another sweep, and both must be able to finish.
func (s *S3Store) Delete(ctx context.Context, hash string) error {
	if len(hash) < 2 {
		return fmt.Errorf("invalid hash")
	}
	_, err := s.cli.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(hash)),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("s3: delete %s: %w", hash, err)
	}
	return nil
}

// isNotFound covers the several shapes S3 uses to say "no such object":
// GetObject returns NoSuchKey, HeadObject returns a bare 404 NotFound, and
// implementations differ on which they send.
func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	return errors.As(err, &nf)
}
