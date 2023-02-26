package downloader

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

const (
	SizeHeaderOffset       = int64(0)
	BlockCountHeaderOffset = int64(8)
	DataHeaderSize         = int64(12)
)

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
	blocks        []*TaskBlock
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

func (s *ProgressStore) splitBlocks() error {
	blockSize := s.getBeastBlockSize()
	splitSize := blockSize + int64(s.minBlockSize)
	blockCount := s.blockCount
	badIterator := s.NewBadIterator(s.task.ctx)
	okIterator := s.NewOkIterator(s.task.ctx)
	for {
		value, err := okIterator.Next()
		if err != nil {
			return err
		}
		if value == nil {
			break
		}
		for value.end-value.start+1 >= splitSize {
			var block *TaskBlock
			if block, err = badIterator.Next(); err != nil {
				return err
			}
			if block == nil {
				block = s.newBlock(DataHeaderSize+int64(blockCount*16), 0, 0)
				s.blocks = append(s.blocks, block)
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
	if err := s.WriteInt64(s.size, SizeHeaderOffset); err != nil {
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
		s.blocks = append(s.blocks, block)
	}
	if err := s.WriteInt(s.blockCount, 8); err != nil {
		return err
	}
	return nil
}

type checkFunc func(block *TaskBlock) bool

type BlockIterator struct {
	atomic.Int32
	ctx    context.Context
	check  checkFunc
	blocks []*TaskBlock
}

func (it *BlockIterator) Next() (*TaskBlock, error) {
	for it.ctx.Err() == nil {
		offset := it.Add(1)
		if int(offset) < len(it.blocks) {
			block := it.blocks[offset]
			if it.check(block) {
				return block, nil
			}
		} else {
			break
		}
	}
	return nil, it.ctx.Err()
}

func (s *ProgressStore) NewIterator(ctx context.Context, check checkFunc) *BlockIterator {
	iterator := &BlockIterator{
		blocks: s.blocks,
		ctx:    ctx,
		check:  check,
	}
	iterator.Store(-1)
	return iterator
}

func (s *ProgressStore) NewBadIterator(ctx context.Context) *BlockIterator {
	return s.NewIterator(ctx, func(block *TaskBlock) bool {
		return block.start > block.end
	})
}

func (s *ProgressStore) NewOkIterator(ctx context.Context) *BlockIterator {
	return s.NewIterator(ctx, func(block *TaskBlock) bool {
		return block.start <= block.end
	})
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

func (s *ProgressStore) loadBlocks() (ok bool, err error) {
	var size int64
	size, err = s.ReadInt64(SizeHeaderOffset)
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = s.generateData()
			ok = true
		}
		return
	} else if size != s.size {
		err = fmt.Errorf("data size %d not match %d: %w", size, s.size, InvalidProgressData)
		return
	}
	s.blockCount, err = s.ReadInt(BlockCountHeaderOffset)
	if err != nil {
		return
	}
	if s.blockCount <= 0 {
		err = fmt.Errorf("block count %d must great than zero: %w", s.blockCount, InvalidProgressData)
		return
	}
	offset := DataHeaderSize
	s.blocks = make([]*TaskBlock, s.blockCount)
	for i := 0; i < s.blockCount; i++ {
		block := s.newBlock(offset, 0, 0)
		if block.start, err = s.ReadInt64(offset); err == nil {
			offset += 8
			block.end, err = s.ReadInt64(offset)
			offset += 8
		}
		if err != nil {
			break
		}
		s.blocks[i] = block
	}
	return
}

func (s *ProgressStore) init() error {
	if ok, err := s.loadBlocks(); err != nil {
		return fmt.Errorf("failed to load blocks from store: %w", err)
	} else if ok {
		return nil
	}
	var blocks []*TaskBlock
	for _, block := range s.blocks {
		if block.start <= block.end {
			blocks = append(blocks, block)
		}
	}
	if err := MergeBlocks(blocks); err != nil {
		return err
	}
	var uncompletedSize int64
	for _, block := range blocks {
		size := block.end - block.start + 1
		if size > 0 {
			uncompletedSize += size
		}
	}
	if uncompletedSize > s.size {
		return fmt.Errorf("uncompleted size %d bigger than total file size %d: %w", uncompletedSize, s.size, InvalidProgressData)
	} else {
		s.completedSize = s.size - uncompletedSize
	}
	return s.splitBlocks()
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
