package downloader

import "io"

type RandomReadWriter interface {
	io.WriterAt
	io.ReaderAt
	io.Closer
	Truncate(size int64) error
}
