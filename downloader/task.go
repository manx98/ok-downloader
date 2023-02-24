package downloader

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

const (
	Waiting  = "waiting"
	Running  = "Running"
	Paused   = "Paused"
	Failed   = "Failed"
	Finished = "Finished"
)

type DownloadTask struct {
	id              string
	maxWorkers      int
	ctx             context.Context
	cancel          context.CancelFunc
	m               *DownloadManager
	dataStore       RandomReadWriter
	progressStore   *ProgressStore
	totalWrite      atomic.Int64
	activeThreads   atomic.Int32
	status          string
	eventHandler    *EventHandler
	group           sync.WaitGroup
	errorValue      atomic.Value
	blockChan       chan *TaskBlock
	links           []*Link
	requireChan     chan *TaskBlock
	providerChan    chan *TaskBlock
	statusProcessor *DownloadTaskStatusProcessor
}

func NewTask(options *DownloadTaskOptions, m *DownloadManager) (*DownloadTask, error) {
	if u, err := uuid.NewUUID(); err != nil {
		return nil, err
	} else {
		task := &DownloadTask{
			id:           u.String(),
			m:            m,
			dataStore:    options.dataStore,
			maxWorkers:   options.maxWorkers,
			status:       Waiting,
			eventHandler: options.eventHandler,
			links:        options.links,
			requireChan:  make(chan *TaskBlock),
			providerChan: make(chan *TaskBlock),
		}
		task.ctx, task.cancel = context.WithCancel(m.ctx)
		task.progressStore, err = newProgressStore(options.size, options.minBlockSize, options.maxBlockSize, options.maxWorkers, options.progressStore, task)
		if err == nil {
			task.statusProcessor = NewDownloadTaskStatusProcessor(task)
		}
		return task, err
	}
}

func NewTaskToLocal(options *DownloadTaskOptions, m *DownloadManager, progressFilePath, dataFilePath string) (task *DownloadTask, err error) {
	options.progressStore, err = os.OpenFile(progressFilePath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return nil, err
	}
	options.dataStore, err = os.OpenFile(dataFilePath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		if err1 := options.progressStore.Close(); err1 != nil {
			log.Printf("failed to close progress [%s] store: %v", progressFilePath, err1)
		}
		return nil, err
	}
	return NewTask(options, m)
}

func (t *DownloadTask) GetID() string {
	return t.id
}

func (t *DownloadTask) GetTotalDownload() int64 {
	return t.totalWrite.Load()
}

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

func (t *DownloadTask) GetStatus() string {
	return t.status
}

func (t *DownloadTask) Close() {
	t.cancel()
	t.group.Wait()
	if err := t.dataStore.Close(); err != nil {
		log.Printf("error closing data store: %v", err)
	}
	if err := t.progressStore.Close(); err != nil {
		log.Printf("error closing progress store: %v", err)
	}
	close(t.providerChan)
	close(t.requireChan)
}

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

func (t *DownloadTask) Run() {
	t.status = Running
	iterator := t.progressStore.newIterator(t.ctx)
	found := true
	for found {
		found = false
		for _, link := range t.links {
			if link.maxWorkers > 0 {
				processor := NewDownloadProcessor(
					iterator,
					t,
					NewHttpDownloadHandler(t.m.httpClient, link, t.threadWatcher),
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
	go t.statusProcessor.Run()
	defer t.statusProcessor.Close()
	t.group.Wait()
	size, err := t.GetCompletedSize()
	if err != nil {
		if err01 := t.GetError(); err01 == nil {
			t.storeError(err, false)
		}
		log.Printf("Error getting completed size: %v", err)
		t.status = Failed
	} else if size == t.GetSize() {
		t.status = Finished
	} else if t.GetError() != nil {
		t.status = Failed
	} else {
		t.status = Paused
	}
}

func (t *DownloadTask) GetSize() int64 {
	return t.progressStore.size
}

func (t *DownloadTask) GetError() error {
	val := t.errorValue.Load()
	if val != nil {
		return val.(error)
	}
	return nil
}

func (t *DownloadTask) storeError(err error, exit bool) {
	if err != nil && errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("download task occurred error: %v", err)
	t.errorValue.Store(err)
	if exit {
		t.cancel()
	}
}

func (t *DownloadTask) GetContext() context.Context {
	return t.ctx
}

func (t *DownloadTask) GetStatusInfo() *DownloadTaskStatus {
	return t.statusProcessor.GetInfo()
}
