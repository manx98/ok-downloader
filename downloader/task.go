package downloader

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type DownloadTask struct {
	id         string
	maxWorkers int
	context.Context
	cancel          context.CancelFunc
	dataStore       RandomReadWriter
	progressStore   *ProgressStore
	totalWrite      atomic.Int64
	activeThreads   atomic.Int32
	status          string
	eventHandler    *EventHandler
	group           sync.WaitGroup
	done            sync.WaitGroup
	errorValue      atomic.Value
	blockChan       chan *TaskBlock
	links           []*Link
	requireChan     chan *TaskBlock
	providerChan    chan *TaskBlock
	statusProcessor *DownloadTaskStatusProcessor
	httpClient      *http.Client
}

func NewTask(optionsProvider DownloadTaskOptionsProvider) (*DownloadTask, error) {
	options := optionsProvider()
	if u, err := uuid.NewUUID(); err != nil {
		return nil, err
	} else {
		task := &DownloadTask{
			id:           u.String(),
			dataStore:    options.dataStore,
			maxWorkers:   options.maxWorkers,
			status:       Waiting,
			eventHandler: options.eventHandler,
			links:        options.links,
			requireChan:  make(chan *TaskBlock),
			providerChan: make(chan *TaskBlock),
			httpClient:   options.httpClient,
		}
		task.Context, task.cancel = context.WithCancel(context.Background())
		task.progressStore, err = newProgressStore(options.size, options.minBlockSize, options.maxBlockSize, options.maxWorkers, options.progressStore, task)
		if err == nil {
			task.statusProcessor = NewDownloadTaskStatusProcessor(task)
		}
		return task, err
	}
}

// GetID get the id of the task
func (t *DownloadTask) GetID() string {
	return t.id
}

// GetTotalDownload Get the size of the downloaded data
func (t *DownloadTask) GetTotalDownload() int64 {
	return t.totalWrite.Load()
}

// GetCompletedSize Get the completed size of the downloaded task
func (t *DownloadTask) GetCompletedSize() (int64, error) {
	unCompletedSize := int64(0)
	for iterator := t.progressStore.newIterator(context.Background()); iterator.hasNext(); {
		block, err := iterator.next()
		if err != nil {
			return 0, fmt.Errorf("failed to load completed size: %w", err)
		}
		if block.start <= block.end {
			unCompletedSize += block.end - block.start + 1
		}
	}
	return t.progressStore.size - unCompletedSize, nil
}

// GetStatus get the status of the task
func (t *DownloadTask) GetStatus() string {
	return t.status
}

// Close closes the download task
func (t *DownloadTask) Close() {
	t.cancel()
	t.group.Wait()
	t.done.Wait()
	if err := t.dataStore.Close(); err != nil {
		if !errors.Is(err, fs.ErrClosed) {
			log.Printf("error closing data store: %v", err)
		}
	}
	if err := t.progressStore.Close(); err != nil {
		if !errors.Is(err, fs.ErrClosed) {
			log.Printf("error closing progress store: %v", err)
		}
	}
	RecoverApplyFunc(func() { close(t.providerChan) })
	RecoverApplyFunc(func() { close(t.requireChan) })
}

// GetThreadCount Get the number of currently active download threads
func (t *DownloadTask) GetThreadCount() int32 {
	return t.activeThreads.Load()
}

func (t *DownloadTask) threadWatcher(in bool) {
	if in {
		t.activeThreads.Add(1)
	} else {
		size, err := t.GetCompletedSize()
		if err != nil {
			t.storeError(err, true)
		}
		t.activeThreads.Add(-1)
		if size == t.GetSize() {
			t.cancel()
		}
	}
}

func (t *DownloadTask) handFinalStatus() {
	t.cancel()
	t.group.Wait()
	t.statusProcessor.calculate()
	info := t.GetStatusInfo()
	if info.Size == info.CompletedSize {
		t.status = Finished
	} else if t.GetError() != nil {
		t.status = Failed
	} else {
		t.status = Paused
	}
}

func (t *DownloadTask) doCalculateStatus() {
	defer t.done.Done()
	defer t.handFinalStatus()
	tik := time.Tick(1 * time.Second)
	for {
		select {
		case <-tik:
			t.statusProcessor.calculate()
			status := t.GetStatusInfo()
			if status.CompletedSize == status.Size {
				return
			}
		case <-t.Done():
			return
		}
	}
}

// Run  Execute the current download task
func (t *DownloadTask) Run() {
	t.eventHandler.OnStart(t)
	defer func() {
		t.eventHandler.OnFinal(t, t.GetError())
		if err := recover(); err != nil {
			t.eventHandler.OnPanic(t, err)
		}
	}()
	t.status = Running
	iterator := t.progressStore.newIterator(t)
	found := true
	for found {
		found = false
		for _, link := range t.links {
			if link.maxWorkers > 0 {
				processor := NewDownloadProcessor(
					iterator,
					t,
					NewHttpDownloadHandler(t.httpClient, link, t.threadWatcher),
				)
				t.group.Add(1)
				go func() {
					defer t.group.Done()
					processor.Run()
				}()
				link.maxWorkers--
				if link.maxWorkers > 0 {
					found = true
				}
			}
		}
	}
	t.done.Add(1)
	go t.doCalculateStatus()
	t.group.Wait()
	t.cancel()
	t.done.Wait()
}

// GetSize Get the total size of the files to be downloaded
func (t *DownloadTask) GetSize() int64 {
	return t.progressStore.size
}

// GetError Get the current download exceptions
func (t *DownloadTask) GetError() error {
	val := t.errorValue.Load()
	if val != nil {
		return val.(error)
	}
	return nil
}

func (t *DownloadTask) storeError(err error, exit bool) {
	if shouldIgnoreError(err) {
		return
	}
	log.Printf("download task occurred error: %v", err)
	t.errorValue.Store(err)
	if exit {
		t.cancel()
	}
}

// GetStatusInfo Get the status information of the current download task
func (t *DownloadTask) GetStatusInfo() *DownloadTaskStatus {
	return t.statusProcessor.GetInfo()
}
