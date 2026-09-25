package store

import "context"

// BlobClaims is the metadata half of a blob write, satisfying blob.Claims.
// It must be built on Queries over the pool, never a transaction: a claim is
// only a pin if a concurrent GC sweep can see it before the bytes land.
type BlobClaims struct{ q *Queries }

// BlobClaims returns the claims a blob store records its writes through.
func (q *Queries) BlobClaims() BlobClaims { return BlobClaims{q: q} }

// Claim pins hash against collection and reports whether the stored copy is
// known damaged and must be overwritten.
func (c BlobClaims) Claim(ctx context.Context, hash string, size int64) (bool, error) {
	return c.q.ClaimBlob(ctx, ClaimBlobParams{Hash: hash, Size: size})
}

// Healed records that a damaged copy has been replaced by good bytes.
func (c BlobClaims) Healed(ctx context.Context, hash string) error {
	return c.q.MarkBlobHealed(ctx, hash)
}
