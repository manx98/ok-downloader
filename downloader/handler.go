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

func NewHttpDownloadHandler(client *http.Client, link *Link, watcher HandlerWatcher) DownloadHandler {
	return func(ctx context.Context, block *TaskBlock) error {
		watcher(true)
		defer watcher(false)
		ct, cancel := context.WithCancel(ctx)
		defer cancel()
		if req, err := link.createRequest(ct, block.start, block.end); err != nil {
			return err
		} else if rsp, err := client.Do(req); err != nil {
			return err
		} else {
			defer func() {
				if err := rsp.Body.Close(); err != nil {
					log.Printf("failed to close request body: %v", err)
				}
			}()
			if !isSuccessResponse(rsp) {
				return fmt.Errorf("server responded with bad status code %d: %w", rsp.StatusCode, BadResponse)
			}
			_, err = io.Copy(block, rsp.Body)
			if err != nil {
				if !errors.Is(err, WriteAlreadyFinished) {
					return err
				}
			}
		}
		return nil
	}
}

type EventHandler struct {
	OnStart func(task *DownloadTask)
	OnFinal func(task *DownloadTask, err error)
	OnPanic func(task *DownloadTask, err any)
}
