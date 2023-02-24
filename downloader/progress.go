package downloader

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
)

const DataHeaderSize = int64(12)

// ProgressStore
//
//		data structure:
//			data length      |      8     |       4        |         8            |       8            |
//	    	data offset      |      0     |       8        |         x            |      x + 8         |
//			data description | total size |  block count   |   block start offset |   block end offset |
type ProgressStore struct {
	store         RandomReadWriter
	task          *DownloadTask
	blockCount    int
	size          int64
	minBlockSize  int
	maxBlockSize  int
	minBlockCount int
	completedSize int64
}

func (s *ProgressStore) ReadInt64(seek int64) (int64, error) {
	cache := make([]byte, 8)
	if _, err := s.store.ReadAt(cache, seek); err != nil {
		return 0, err
	} else {
		return int64(binary.BigEndian.Uint64(cache)), nil
	}
}

func (s *ProgressStore) WriteInt64(value int64, seek int64) error {
	cache := binary.BigEndian.AppendUint64(nil, uint64(value))
	_, err := s.store.WriteAt(cache, seek)
	return err
}

func (s *ProgressStore) ReadInt(seek int64) (int, error) {
	cache := make([]byte, 4)
	if _, err := s.store.ReadAt(cache, seek); err != nil {
		return 0, err
	} else {
		return int(binary.BigEndian.Uint32(cache)), nil
	}
}

func (s *ProgressStore) WriteInt(value int, seek int64) error {
	cache := binary.BigEndian.AppendUint32(nil, uint32(value))
	_, err := s.store.WriteAt(cache, seek)
	return err
}

func (s *ProgressStore) mergeBlocks(blocks []*TaskBlock) (err error) {
	sort.SliceIsSorted(blocks, func(i, j int) bool {
		if blocks[i].start == blocks[j].start {
			return blocks[i].end > blocks[j].end
		}
		return blocks[i].start < blocks[j].start
	})
	if len(blocks) > 1 {
		before := blocks[0]
		for i := 1; i < len(blocks); i++ {
			now := blocks[i]
			clean := (before.start <= now.start && before.end >= now.start && before.end >= now.end) ||
				(before.end+1 == now.start) ||
				(before.start <= now.start && before.end >= now.start && before.end <= now.end)
			if clean {
				if now.end > before.end {
					before.end = now.end
					if err = before.FlushEnd(); err != nil {
						return err
					}
				}
				now.start = now.end + 1
				if err = now.FlushStart(); err != nil {
					return err
				}
			} else {
				before = blocks[i]
			}
		}
	}
	return
}

func (s *ProgressStore) splitBlocks(okBlocks, badBlocks []*TaskBlock) error {
	blockSize := s.getBeastBlockSize()
	splitSize := blockSize + int64(s.minBlockSize)
	blockCount := s.blockCount
	for _, value := range okBlocks {
		for value.end-value.start+1 >= splitSize {
			var block *TaskBlock
			if len(badBlocks) > 0 {
				block = badBlocks[0]
				badBlocks = badBlocks[1:]
			} else {
				block = s.newBlock(DataHeaderSize+int64(blockCount*16), 0, 0)
				blockCount += 1
			}
			block.end = value.end
			value.end = value.start + blockSize - 1
			if err := value.FlushEnd(); err != nil {
				return err
			}
			block.start = value.end + 1
			if err := block.FlushAll(); err != nil {
				return err
			}
			value = block
		}
	}
	if s.blockCount != blockCount {
		s.blockCount = blockCount
		if err := s.WriteInt(blockCount, 8); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProgressStore) newBlock(offset, start, end int64) *TaskBlock {
	return &TaskBlock{
		store:        s,
		start:        start,
		end:          end,
		offset:       offset,
		dataStore:    s.task.dataStore,
		totalWrite:   &s.task.totalWrite,
		ctx:          s.task.ctx,
		providerChan: s.task.providerChan,
		requireChan:  s.task.requireChan,
	}
}

func (s *ProgressStore) generateData() error {
	blockSize := s.getBeastBlockSize()
	start := int64(0)
	offset := DataHeaderSize
	if err := s.WriteInt64(s.size, 0); err != nil {
		return err
	}
	for start < s.size {
		block := s.newBlock(offset, start, 0)
		start += blockSize
		if start+int64(s.minBlockSize) > s.size {
			start = s.size
		}
		block.end = start - 1
		offset += 16
		s.blockCount += 1
		if err := block.FlushAll(); err != nil {
			return err
		}
	}
	if err := s.WriteInt(s.blockCount, 8); err != nil {
		return err
	}
	return nil
}

type BlockIterator struct {
	store  *ProgressStore
	offset int64
	total  int64
	mtx    sync.Mutex
	ctx    context.Context
}

func (it *BlockIterator) next() (block *TaskBlock, err error) {
	if !it.hasNext() {
		return nil, nil
	}
	it.mtx.Lock()
	defer it.mtx.Unlock()
	offset := it.offset*16 + DataHeaderSize
	block = it.store.newBlock(offset, 0, 0)
	if block.start, err = it.store.ReadInt64(offset); err == nil {
		offset += 8
		block.end, err = it.store.ReadInt64(offset)
	}
	if err != nil {
		return
	}
	it.offset += 1
	return block, it.ctx.Err()
}

func (it *BlockIterator) hasNext() bool {
	it.mtx.Lock()
	defer it.mtx.Unlock()
	return it.total > it.offset
}

func (s *ProgressStore) newIterator(ctx context.Context) *BlockIterator {
	return &BlockIterator{
		store: s,
		total: int64(s.blockCount),
		ctx:   ctx,
	}
}

func (s *ProgressStore) getBeastBlockSize() int64 {
	blockSize := (s.size - s.completedSize) / int64(s.minBlockCount)
	if blockSize < int64(s.minBlockSize) {
		blockSize = int64(s.minBlockSize)
	} else if blockSize > int64(s.maxBlockSize) {
		blockSize = int64(s.maxBlockSize)
	}
	return blockSize
}

func (s *ProgressStore) init() error {
	var offset int64
	size, err := s.ReadInt64(offset)
	offset += 8
	if err != nil {
		if errors.Is(err, io.EOF) {
			return s.generateData()
		} else {
			return err
		}
	} else if size != s.size {
		return fmt.Errorf("data size %d not match %d: %w", size, s.size, InvalidProgressData)
	}
	s.blockCount, err = s.ReadInt(offset)
	if err != nil {
		return err
	}
	if s.blockCount <= 0 {
		return fmt.Errorf("block count %d must great than zero: %w", s.blockCount, InvalidProgressData)
	}
	offset += 4
	var blocks []*TaskBlock
	var badBlocks []*TaskBlock
	for iterator := s.newIterator(s.task.ctx); iterator.hasNext(); {
		block, err := iterator.next()
		if err != nil {
			return err
		}
		if block.start <= block.end {
			blocks = append(blocks, block)
		} else {
			badBlocks = append(badBlocks, block)
		}
	}
	if err = s.mergeBlocks(blocks); err != nil {
		return err
	}
	var uncompletedSize int64
	var okBlocks []*TaskBlock
	for _, block := range blocks {
		size := block.end - block.start + 1
		if size > 0 {
			uncompletedSize += size
			okBlocks = append(okBlocks, block)
		} else {
			badBlocks = append(badBlocks, block)
		}
	}
	if uncompletedSize > s.size {
		return fmt.Errorf("uncompleted size %d bigger than total file size %d: %w", uncompletedSize, s.size, InvalidProgressData)
	} else {
		s.completedSize = s.size - uncompletedSize
	}
	return s.splitBlocks(okBlocks, badBlocks)
}

func (s *ProgressStore) Close() error {
	return s.store.Close()
}

func newProgressStore(size int64, minBlockSize, maxBlockSize, minBlockCount int, store RandomReadWriter, task *DownloadTask) (*ProgressStore, error) {
	p := &ProgressStore{
		store:         store,
		size:          size,
		minBlockSize:  minBlockSize,
		maxBlockSize:  maxBlockSize,
		minBlockCount: minBlockCount,
		task:          task,
	}
	if err := p.init(); err != nil {
		return nil, err
	} else {
		return p, nil
	}
}
