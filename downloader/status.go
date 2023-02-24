package downloader

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type DownloadTaskStatus struct {
	Size          int64
	CompletedSize int64
	Status        string
	Speed         int64
	Threads       int32
	Err           error
}

type DownloadTaskStatusProcessor struct {
	atomic.Value
	lastCalculatedTime time.Time
	lastDownloadedSize int64
	task               *DownloadTask
	ctx                context.Context
	cancel             context.CancelFunc
	sync.WaitGroup
}

func NewDownloadTaskStatusProcessor(task *DownloadTask) *DownloadTaskStatusProcessor {
	processor := &DownloadTaskStatusProcessor{
		task:               task,
		lastCalculatedTime: time.Now(),
	}
	processor.Add(1)
	processor.calculate()
	processor.ctx, processor.cancel = context.WithCancel(context.Background())
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
		Size:          r.task.GetSize(),
		CompletedSize: size,
		Threads:       r.task.GetThreadCount(),
		Status:        r.task.GetStatus(),
		Speed:         (totalDownload - r.lastDownloadedSize) * 1000 / (now.Sub(r.lastCalculatedTime).Milliseconds() + 1),
		Err:           r.task.GetError(),
	})
	r.lastCalculatedTime = now
	r.lastDownloadedSize = totalDownload
}

func (r *DownloadTaskStatusProcessor) GetInfo() *DownloadTaskStatus {
	return r.Load().(*DownloadTaskStatus)
}

func (r *DownloadTaskStatusProcessor) Run() {
	defer r.Done()
	tick := time.Tick(1 * time.Second)
	for {
		select {
		case <-r.ctx.Done():
			r.calculate()
			return
		case <-tick:
			r.calculate()
		}
	}
}

func (r *DownloadTaskStatusProcessor) Close() {
	r.cancel()
	r.Wait()
}
