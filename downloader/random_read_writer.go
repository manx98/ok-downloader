package downloader

type RandomReadWriter interface {
	ReadAt(data []byte, offset int64) (int, error)
	WriteAt(data []byte, offset int64) (int, error)
	Close() error
	Sync() error
	Truncate(size int64) error
}
