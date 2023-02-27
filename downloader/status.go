package downloader

import (
	"sync/atomic"
	"time"
)

const (
	Waiting = "Waiting"
	Running = "Running"
	Paused  = "Paused"
	Failed  = "Failed"
	Success = "Success"
)

type DownloadTaskStatus struct {
	Size          int64
	CompletedSize int64
	Status        string // Status of the download(Waiting、Running、Paused、Failed、Success)
	Speed         int64  // Download speed (bytes per second)
	Threads       int32
	Err           error
}

type downloadTaskStatusProcessor struct {
	atomic.Value
	lastCalculatedTime time.Time
	lastDownloadedSize int64
	task               *DownloadTask
}

func newDownloadTaskStatusProcessor(task *DownloadTask) *downloadTaskStatusProcessor {
	processor := &downloadTaskStatusProcessor{
		task:               task,
		lastCalculatedTime: time.Now(),
	}
	processor.calculate()
	return processor
}

func (r *downloadTaskStatusProcessor) calculate() {
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

func (r *downloadTaskStatusProcessor) GetInfo() *DownloadTaskStatus {
	status := r.Load().(*DownloadTaskStatus)
	status.Status = r.task.GetStatus()
	status.Size = r.task.GetSize()
	status.Threads = r.task.GetThreadCount()
	status.Err = r.task.GetError()
	return status
}
