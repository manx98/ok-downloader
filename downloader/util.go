package downloader

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func ResolveContentRange(contentRange string) (int64, error) {
	if contentRange != "" {
		r := strings.Split(contentRange, "/")
		if len(r) != 2 {
			return -1, fmt.Errorf("invalid content range \"%s\"", contentRange)
		}
		contentLength, err := strconv.ParseInt(r[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("error parsing content length from \"%s\": %w", r[1], err)
		}
		return contentLength, nil
	}
	return 0, fmt.Errorf("content-length not exist in response header")
}

func MergeBlocks(blocks []*TaskBlock) (err error) {
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].start == blocks[j].start {
			return blocks[i].end > blocks[j].end
		}
		return blocks[i].start < blocks[j].start
	})
	if len(blocks) > 1 {
		before := blocks[0]
		for i := 1; i < len(blocks); i++ {
			now := blocks[i]
			clean := (before.start <= now.start && before.end >= now.start && before.end >= now.end) ||
				(before.end+1 == now.start) ||
				(before.start <= now.start && before.end >= now.start && before.end <= now.end)
			if clean {
				if now.end > before.end {
					before.end = now.end
					if err = before.FlushEnd(); err != nil {
						return err
					}
				}
				now.start = now.end + 1
				if err = now.FlushStart(); err != nil {
					return err
				}
			} else {
				before = blocks[i]
			}
		}
	}
	return
}

func RecoverGoroutine(f func()) {
	go RecoverApplyFunc(f)
}

func RecoverApplyFunc(f func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[recovered] call function occur panic: %v", err)
		}
	}()
	f()
}

func shouldIgnoreError(err error) bool {
	if err == nil {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		err.Error() == "context deadline exceeded (Client.Timeout or context cancellation while reading body)"
}

func ResolveFileSize(httpClient *http.Client, link *Link) (int64, error) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	request, err := link.createRequest(ctx, 0, 0)
	if err != nil {
		return 0, err
	}
	if resp, err := httpClient.Do(request); err != nil {
		return 0, err
	} else {
		defer func() {
			if err = resp.Body.Close(); err != nil {
				log.Printf("Error closing request: %v", err)
			}
		}()
		if isSuccessResponse(resp) {
			return ResolveContentRange(resp.Header.Get("Content-Range"))
		} else {
			return 0, BadResponse
		}
	}
}
