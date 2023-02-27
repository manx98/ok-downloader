package main

import (
	"github.com/manx98/ok-downloader/downloader"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	fileName := "go1.20.1.windows-amd64.zip"
	downloadURL := "https://go.dev/dl/go1.20.1.windows-amd64.zip"
	links := []*downloader.Link{
		downloader.NewLink(downloadURL, 10, nil),
		downloader.NewLink(downloadURL, 10, nil),
		downloader.NewLink(downloadURL, 10, nil),
	}
	options := downloader.NewDownloadTaskOptionsBuilder()
	options.SetMaxBlockSize(1024 * 1024 * 10)
	options.SetMinBlockSize(1024)
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConnsPerHost:   math.MaxInt,
		},
	}
	options.SetHttpClient(httpClient)
	size, err := downloader.ResolveFileSize(httpClient, links[0])
	if err != nil {
		log.Fatalf("failed to resolve file size: %v", err)
	} else {
		options.SetSize(size)
	}
	dataFile := fileName + ".tmp"
	progressFile := fileName + ".dat"
	if err = options.SetProgressStoreByPath(progressFile); err != nil {
		log.Fatalf("failed to set progress store by path: %v", err)
	}
	if err = options.SetDataStoreByPath(dataFile); err != nil {
		log.Fatalf("failed to set data store by path: %v", err)
	}
	provider, err := options.Build(links...)
	if err != nil {
		log.Fatalf("failed to build provider: %v", err)
	}
	task, err := downloader.NewTask(provider)
	if err != nil {
		log.Fatalf("error creating task: %v", err)
	}
	go func() {
		tk := time.Tick(3 * time.Second)
		for {
			select {
			case <-tk:
				info := task.GetStatusInfo()
				log.Printf("Downloading[status=%s][speed=%dKB/s][threads=%d][%.2f%s]: %v", info.Status, info.Speed/1024, info.Threads, float64(info.CompletedSize*100)/float64(info.Size), "%", info.Err)
			case <-task.Done():
				return
			}
		}
	}()
	task.Run()
	info := task.GetStatusInfo()
	task.Close()
	if info.Status == downloader.Success {
		if err = os.Remove(progressFile); err != nil {
			log.Fatalf("Error removing progress file: %v", err)
		}
		if err = os.Rename(dataFile, fileName); err != nil {
			log.Fatalf("Error renaming data file \"%s\" -> \"%s\": %v", dataFile, fileName, err)
		}
	}
	log.Printf("Exit Download[status=%s][speed=%dKB/s][threads=%d][%.2f%s]: %v", info.Status, info.Speed/1024, info.Threads, float64(info.CompletedSize*100)/float64(info.Size), "%", info.Err)
}
