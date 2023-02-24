package downloader

import (
	"context"
	"log"
	"net/http"
	"sync"
)

type DownloadManager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	maxWorkers int
	tasks      sync.Map
	taskChan   chan *DownloadTask
	sync.WaitGroup
	httpClient *http.Client
}

func NewDownloadManager(httpClient *http.Client, maxWorkers int) *DownloadManager {
	manager := &DownloadManager{
		maxWorkers: maxWorkers,
		taskChan:   make(chan *DownloadTask, 3),
		httpClient: httpClient,
	}
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	return manager
}

func (m *DownloadManager) registerTask(task *DownloadTask) {
	m.tasks.Store(task.id, task)
}

func (m *DownloadManager) AddTask(task *DownloadTask) {
	m.registerTask(task)
	go func() {
		select {
		case <-m.ctx.Done():
			return
		case m.taskChan <- task:
		}
	}()
}

func (m *DownloadManager) ResolveFileSize(link *Link) (int64, error) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	request, err := link.createRequest(ctx, 0, 0)
	if err != nil {
		return 0, err
	}
	if resp, err := m.httpClient.Do(request); err != nil {
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

func handleTask(task *DownloadTask) {
	defer func() {
		task.eventHandler.OnFinal(task, task.GetError())
		if err := recover(); err != nil {
			log.Printf("resolve task %s occurred panic: %v", task.id, err)
			task.eventHandler.OnPanic(task, err)
		}
	}()
	task.eventHandler.OnStart(task)
	task.Run()
}

func (m *DownloadManager) Run() {
	m.Add(m.maxWorkers)
	for i := 0; i < m.maxWorkers; i++ {
		go func() {
			defer m.Done()
			for {
				select {
				case <-m.ctx.Done():
					return
				case task := <-m.taskChan:
					handleTask(task)
				}
			}
		}()
	}
}

func (m *DownloadManager) Close() {
	m.cancel()
	m.Wait()
}
