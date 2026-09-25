package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

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
	spool  string
	claims Claims
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

// spoolPrefix names Put's buffers in the spool directory; Tidy collects the
// ones a crashed process left.
const spoolPrefix = "pp-blob-"

func NewS3Store(ctx context.Context, cfg S3Config, claims Claims) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket is required")
	}
	if claims == nil {
		return nil, errors.New("blob: a store needs claims to record its writes")
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
	return &S3Store{cli: cli, bucket: cfg.Bucket, spool: cfg.Spool, claims: claims}, nil
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
	tmp, err := os.CreateTemp(s.spool, spoolPrefix+"*")
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
	if err := s.upload(ctx, tmp, hash, size); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// Ingest hashes a staged file, uploads it under its hash, and removes it.
// Unlike LocalStore there is no rename to make: the file is read twice, once
// to learn the key and once to send it.
func (s *S3Store) Ingest(ctx context.Context, path string) (string, int64, error) {
	hash, size, err := hashFile(path)
	if err != nil {
		return "", 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	err = s.upload(ctx, f, hash, size)
	_ = f.Close()
	if err != nil {
		return "", 0, err
	}
	if err := os.Remove(path); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// upload claims hash, then sends a seekable body to its content address —
// or skips the request when an intact copy is already there. The claim comes
// first for the reason LocalStore.install gives.
func (s *S3Store) upload(ctx context.Context, body io.ReadSeeker, hash string, size int64) error {
	replace, err := s.claims.Claim(ctx, hash, size)
	if err != nil {
		return fmt.Errorf("claim blob: %w", err)
	}
	if !replace {
		stored, err := s.storedSize(ctx, hash)
		switch {
		case err != nil:
			return err
		case stored == size:
			// Dedup: identical content is already at this address, and
			// re-uploading would only cost bandwidth.
			return nil
		case stored >= 0:
			slog.WarnContext(ctx, "blob: stored copy has the wrong size, replacing it",
				"hash", short(hash), "stored", stored, "size", size)
			replace = true
		}
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = s.cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.key(hash)),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3: put %s: %w", hash, err)
	}
	if replace {
		if err := s.claims.Healed(ctx, hash); err != nil {
			slog.WarnContext(ctx, "blob: healed copy not recorded", "hash", short(hash), "err", err)
		}
	}
	return nil
}

// storedSize is the length of the object at hash's address, or -1 when there
// is none.
func (s *S3Store) storedSize(ctx context.Context, hash string) (int64, error) {
	out, err := s.cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(hash)),
	})
	if err == nil {
		return aws.ToInt64(out.ContentLength), nil
	}
	if isNotFound(err) {
		return -1, nil
	}
	return 0, fmt.Errorf("s3: head %s: %w", hash, err)
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

// Check reports whether the bucket is reachable. HeadBucket is a cheap,
// side-effect-free call that fails exactly when the bucket is missing, the
// credentials are wrong, or the endpoint is unreachable — the three ways an
// S3-backed deployment goes unusable.
func (s *S3Store) Check(ctx context.Context) error {
	_, err := s.cli.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("s3: head bucket: %w", err)
	}
	return nil
}

// Walk lists the bucket page by page. Keys that are not <2-hex>/<hash> are
// someone else's and are skipped.
func (s *S3Store) Walk(ctx context.Context, fn func(hash string, size int64) error) error {
	pages := s3.NewListObjectsV2Paginator(s.cli, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("s3: list: %w", err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if len(key) != 67 || key[2] != '/' || !IsHash(key[3:]) || key[:2] != key[3:5] {
				continue
			}
			if err := fn(key[3:], aws.ToInt64(obj.Size)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Tidy collects Put's abandoned spool files. Nothing temporary is ever
// written to the bucket itself: an object only appears once PutObject has
// all of it.
func (s *S3Store) Tidy(_ context.Context, age time.Duration) (int, error) {
	dir := s.spool
	if dir == "" {
		dir = os.TempDir()
	}
	return removeStale(dir, spoolPrefix, age)
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
