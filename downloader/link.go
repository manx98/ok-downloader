package downloader

import (
	"context"
	"fmt"
	"net/http"
)

type Link struct {
	downloadLink string            // download link to download
	maxWorkers   int               // maximum number of workers
	header       map[string]string // header for download request
}

func NewLink(link string, maxWorkers int, header map[string]string) *Link {
	return &Link{
		downloadLink: link,
		maxWorkers:   maxWorkers,
		header:       header,
	}
}

func (link *Link) setRequestHeader(req *http.Request) {
	if link.header != nil {
		for k, v := range link.header {
			req.Header.Set(k, v)
		}
	}
}

func (link *Link) createRequest(ctx context.Context, start, end int64) (*http.Request, error) {
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.downloadLink, nil); err != nil {
		return nil, err
	} else {
		link.setRequestHeader(req)
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		return req, nil
	}
}
