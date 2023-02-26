package downloader

import (
	"context"
	"fmt"
)

type DownloadHandler func(ctx context.Context, block *TaskBlock) error

type DownloadProcessor struct {
	handler      DownloadHandler
	iterator     *BlockIterator
	requireChan  chan *TaskBlock
	providerChan chan *TaskBlock
	task         *DownloadTask
}

func newDownloadProcessor(iterator *BlockIterator, task *DownloadTask, handler DownloadHandler) *DownloadProcessor {
	return &DownloadProcessor{
		task:         task,
		iterator:     iterator,
		handler:      handler,
		requireChan:  task.requireChan,
		providerChan: task.providerChan,
	}
}

func (p *DownloadProcessor) provider(block *TaskBlock) {
	RecoverGoroutine(func() {
		select {
		case <-p.task.ctx.Done():
			return
		case p.providerChan <- block:
		}
	})
}

func (p *DownloadProcessor) require(block *TaskBlock) {
	RecoverGoroutine(func() {
		select {
		case <-p.task.ctx.Done():
		case p.requireChan <- block:
		}
	})
}

func (p *DownloadProcessor) handle(block *TaskBlock) (exit bool) {
	defer func() {
		if block.start <= block.end {
			p.provider(block)
		}
		if err := recover(); err != nil {
			p.task.storeError(fmt.Errorf("download thread occur painc: %v", err), false)
		}
	}()
	if err := p.handler(p.task.ctx, block); err != nil {
		p.task.storeError(err, false)
		return true
	}
	return false
}

func (p *DownloadProcessor) Run() {
	var lastBlock *TaskBlock
	for {
		block, err := p.iterator.Next()
		if err != nil {
			p.task.storeError(err, true)
			return
		}
		if block == nil {
			break
		}
		lastBlock = block
		if block.start <= block.end {
			if p.handle(block) {
				return
			}
		}
	}
	if lastBlock == nil {
		return
	}
	p.require(lastBlock)
	for {
		select {
		case <-p.task.ctx.Done():
			return
		case lastBlock = <-p.providerChan:
			if p.handle(lastBlock) {
				return
			} else {
				p.require(lastBlock)
			}
		}
	}
}
