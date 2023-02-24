package downloader

import (
	"context"
	"log"
)

type DownloadHandler func(ctx context.Context, block *TaskBlock) error

type DownloadProcessor struct {
	ctx          context.Context
	handler      DownloadHandler
	iterator     *BlockIterator
	requireChan  chan *TaskBlock
	providerChan chan *TaskBlock
	task         *DownloadTask
}

func NewDownloadProcessor(iterator *BlockIterator, task *DownloadTask, handler DownloadHandler) *DownloadProcessor {
	return &DownloadProcessor{
		task:         task,
		iterator:     iterator,
		ctx:          task.ctx,
		handler:      handler,
		requireChan:  task.requireChan,
		providerChan: task.providerChan,
	}
}

func (p *DownloadProcessor) provider(block *TaskBlock) {
	go func() {
		log.Printf("Starting DownloadProcessor[provider]: %v", block)
		select {
		case <-p.ctx.Done():
			return
		case p.providerChan <- block:
			log.Printf("Success DownloadProcessor[provider]: %v", block)
		}
	}()
}

func (p *DownloadProcessor) require(block *TaskBlock) {
	go func() {
		select {
		case <-p.ctx.Done():
		case p.requireChan <- block:
		}
	}()
}

func (p *DownloadProcessor) Run() {
	var lastBlock *TaskBlock
	for {
		block, err := p.iterator.next()
		if err != nil {
			p.task.storeError(err, true)
			return
		}
		if block == nil {
			break
		}
		lastBlock = block
		if block.start < block.end {
			if err = p.handler(p.ctx, block); err != nil {
				p.task.storeError(err, false)
				p.provider(block)
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
		case <-p.ctx.Done():
			return
		case block := <-p.providerChan:
			if err := p.handler(p.ctx, block); err != nil {
				p.task.storeError(err, false)
				p.provider(block)
				return
			} else {
				p.require(lastBlock)
			}
		}
	}
}
