package downloader

import (
	"sync/atomic"
	"time"
)

const (
	Waiting  = "Waiting"
	Running  = "Running"
	Paused   = "Paused"
	Failed   = "Failed"
	Finished = "Finished"
)

type DownloadTaskStatus struct {
	Size          int64
	CompletedSize int64
	Status        string // Status of the download(Waiting、Running、Paused、Failed、Finished)
	Speed         int64  // Download speed (bytes per second)
	Threads       int32
	Err           error
}

type DownloadTaskStatusProcessor struct {
	atomic.Value
	lastCalculatedTime time.Time
	lastDownloadedSize int64
	task               *DownloadTask
}

func NewDownloadTaskStatusProcessor(task *DownloadTask) *DownloadTaskStatusProcessor {
	processor := &DownloadTaskStatusProcessor{
		task:               task,
		lastCalculatedTime: time.Now(),
	}
	processor.calculate()
	return processor
}

func (r *DownloadTaskStatusProcessor) calculate() {
	size, err := r.task.GetCompletedSize()
	if err != nil {
		r.task.storeError(err, true)
		return
	}
	now := time.Now()
	totalDownload := r.task.GetTotalDownload()
	r.Store(&DownloadTaskStatus{
		CompletedSize: size,
		Speed:         (totalDownload - r.lastDownloadedSize) * 1000 / (now.Sub(r.lastCalculatedTime).Milliseconds() + 1),
	})
	r.lastCalculatedTime = now
	r.lastDownloadedSize = totalDownload
}

func (r *DownloadTaskStatusProcessor) GetInfo() *DownloadTaskStatus {
	status := r.Load().(*DownloadTaskStatus)
	status.Status = r.task.GetStatus()
	status.Size = r.task.GetSize()
	status.Threads = r.task.GetThreadCount()
	status.Err = r.task.GetError()
	return status
}
