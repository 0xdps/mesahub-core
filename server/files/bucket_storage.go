// Package files — bucket_storage.go defines the BucketStorage interface for
// bucket file operations. Template ships LocalBucketStorage (local volume);
// the SaaS binary injects S3BucketStorage at startup.
package files

import "io"

// BucketStorage abstracts over the storage backend used by bucket file
// operations. All implementations must be safe for concurrent use.
type BucketStorage interface {
	Upload(in UploadInput) (UploadResult, error)
	GetBlobReader(contentHash string) (io.ReadCloser, error)
	GetBlobBytes(contentHash string) ([]byte, error)
	GetByID(id string) (*StoredFile, error)
	List(namespace string, limit, offset int, folderPrefix, sort, order string) (ListFilesResult, error)
	SumBytesForNamespace(ns string) int64
	DeleteByID(id, namespace string) error
	StorageMetrics() (MetricsResult, error)
}

// LocalBucketStorage wraps *Storage to implement BucketStorage using the local
// volume. This is the default backend shipped with the template binary.
type LocalBucketStorage struct {
	s *Storage
}

// NewLocalBucketStorage returns a BucketStorage backed by the local volume.
func NewLocalBucketStorage(s *Storage) *LocalBucketStorage {
	return &LocalBucketStorage{s: s}
}

func (l *LocalBucketStorage) Upload(in UploadInput) (UploadResult, error) {
	return l.s.Upload(in)
}

func (l *LocalBucketStorage) GetBlobReader(contentHash string) (io.ReadCloser, error) {
	return l.s.GetBlobReader(contentHash)
}

func (l *LocalBucketStorage) GetBlobBytes(contentHash string) ([]byte, error) {
	return l.s.GetBlobBytes(contentHash)
}

func (l *LocalBucketStorage) GetByID(id string) (*StoredFile, error) {
	return l.s.GetByID(id)
}

func (l *LocalBucketStorage) List(namespace string, limit, offset int, folderPrefix, sort, order string) (ListFilesResult, error) {
	return l.s.List(namespace, limit, offset, folderPrefix, sort, order)
}

func (l *LocalBucketStorage) SumBytesForNamespace(ns string) int64 {
	return l.s.SumBytesForNamespace(ns)
}

func (l *LocalBucketStorage) DeleteByID(id, namespace string) error {
	return l.s.DeleteByID(id, namespace)
}

func (l *LocalBucketStorage) StorageMetrics() (MetricsResult, error) {
	return l.s.StorageMetrics()
}
