package downloader

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
)

type TaskBlock struct {
	offset       int64
	start        int64
	end          int64
	store        *ProgressStore
	dataStore    RandomReadWriter
	totalWrite   *atomic.Int64
	ctx          context.Context
	requireChan  chan *TaskBlock
	providerChan chan *TaskBlock
}

func (block *TaskBlock) String() string {
	return fmt.Sprintf("TaskBlock[offset=%d, start=%d, end=%d]", block.offset, block.start, block.end)
}

func (block *TaskBlock) Write(p []byte) (n int, err error) {
	n, err = block.dataStore.WriteAt(p, block.start)
	block.totalWrite.Add(int64(n))
	block.start += int64(n)
	if err = block.checkPoint(); err != nil {
		return
	}
	if err = block.FlushStart(); err != nil {
		return
	}
	if block.start > block.end {
		err = WriteAlreadyFinished
	}
	if err == nil {
		err = block.ctx.Err()
	}
	return
}

func (block *TaskBlock) checkPoint() error {
	l := block.end - block.start + 1
	if l >= int64(block.store.minBlockSize)*2 {
		select {
		case b := <-block.requireChan:
			l /= 2
			b.end = block.end
			block.end = block.start + l - 1
			b.start = block.end + 1
			log.Printf("splited block %v", b)
			if err := b.FlushAll(); err != nil {
				return err
			}
			if err := block.FlushEnd(); err != nil {
				return err
			}
			block.provider(b)
		default:
		}
	}
	return nil
}

func (block *TaskBlock) provider(b *TaskBlock) {
	RecoverGoroutine(func() {
		select {
		case <-block.ctx.Done():
		case block.providerChan <- b:
		}
	})
}

func (block *TaskBlock) FlushStart() (err error) {
	err = block.store.WriteInt64(block.start, block.offset)
	if err != nil {
		err = fmt.Errorf("failed to flush task block start: %w", err)
	}
	return
}

func (block *TaskBlock) FlushEnd() (err error) {
	err = block.store.WriteInt64(block.end, block.offset+8)
	if err != nil {
		err = fmt.Errorf("failed to flush task block end: %w", err)
	}
	return
}

func (block *TaskBlock) FlushAll() (err error) {
	if err = block.FlushStart(); err == nil {
		err = block.FlushEnd()
	}
	if err != nil {
		return fmt.Errorf("failed to FlushAll task block progress: %w", err)
	}
	return
}
