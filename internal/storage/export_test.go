package storage

// SetCopyPartSize lowers the part size of multipart copies, so a test can
// copy a small object in parts as a large video would be.
func SetCopyPartSize(s *S3, n uint64) { s.copyPartSize = n }
