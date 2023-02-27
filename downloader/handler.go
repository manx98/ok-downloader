package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
)

func isSuccessResponse(response *http.Response) bool {
	return response.StatusCode == 200 || response.StatusCode == 206
}

type HandlerWatcher func(in bool)

func newHttpDownloadHandler(client *http.Client, link *Link, watcher HandlerWatcher) DownloadHandler {
	return func(ctx context.Context, block *TaskBlock) error {
		watcher(true)
		defer watcher(false)
		ct, cancel := context.WithCancel(ctx)
		defer cancel()
		req, err := link.createRequest(ct, block.start, block.end)
		if err != nil {
			return err
		}
		rsp, err := client.Do(req)
		defer func() {
			cancel()
			if rsp != nil {
				if err := rsp.Body.Close(); err != nil {
					log.Printf("failed to close request body: %v", err)
				}
			}
		}()
		if err != nil {
			return err
		}
		if !isSuccessResponse(rsp) {
			return fmt.Errorf("server responded with bad status code %d: %w", rsp.StatusCode, BadResponse)
		}
		if _, err = io.Copy(block, rsp.Body); err != nil {
			if !errors.Is(err, WriteAlreadyFinished) {
				return err
			}
		}
		return nil
	}
}
