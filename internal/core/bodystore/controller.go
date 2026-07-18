package bodystore

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrBodyTooLarge  = errors.New("request body exceeds replay store limit")
	ErrStoreCapacity = errors.New("request body replay store capacity is exhausted")
	ErrStoreRetired  = errors.New("request body replay store is retired")
	ErrStoreClosed   = errors.New("request body replay store is closed")
)

const defaultChunkBytes = 64 << 10

type Config struct {
	MemoryMaxBytes     int64
	MemoryPerBodyBytes int64
	DiskMaxBytes       int64
	MaxBodyBytes       int64
	ChunkBytes         int
	TempDir            string
}

type Controller struct {
	config Config

	mu         sync.Mutex
	memoryUsed int64
	diskUsed   int64
	tempOnce   sync.Once
	tempDir    string
	tempErr    error
}

type Snapshot struct {
	MemoryUsedBytes      int64
	MemoryAvailableBytes int64
	DiskUsedBytes        int64
	DiskAvailableBytes   int64
}

func New(config Config) *Controller {
	if config.ChunkBytes <= 0 {
		config.ChunkBytes = defaultChunkBytes
	}
	if config.MemoryPerBodyBytes > config.MaxBodyBytes && config.MaxBodyBytes > 0 {
		config.MemoryPerBodyBytes = config.MaxBodyBytes
	}
	return &Controller{config: config}
}

func (c *Controller) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return Snapshot{
		MemoryUsedBytes:      c.memoryUsed,
		MemoryAvailableBytes: available(c.config.MemoryMaxBytes, c.memoryUsed),
		DiskUsedBytes:        c.diskUsed,
		DiskAvailableBytes:   available(c.config.DiskMaxBytes, c.diskUsed),
	}
}

func available(limit, used int64) int64 {
	remaining := limit - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (c *Controller) tryReserveMemory(size int64) bool {
	if c == nil || size < 0 {
		return false
	}
	if size == 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.MemoryMaxBytes <= 0 || c.memoryUsed+size > c.config.MemoryMaxBytes {
		return false
	}
	c.memoryUsed += size
	return true
}

func (c *Controller) releaseMemory(size int64) {
	if c == nil || size <= 0 {
		return
	}
	c.mu.Lock()
	c.memoryUsed -= size
	if c.memoryUsed < 0 {
		c.memoryUsed = 0
	}
	c.mu.Unlock()
}

func (c *Controller) tryReserveDisk(size int64) bool {
	if c == nil || size < 0 {
		return false
	}
	if size == 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.DiskMaxBytes <= 0 || c.diskUsed+size > c.config.DiskMaxBytes {
		return false
	}
	c.diskUsed += size
	return true
}

func (c *Controller) releaseDisk(size int64) {
	if c == nil || size <= 0 {
		return
	}
	c.mu.Lock()
	c.diskUsed -= size
	if c.diskUsed < 0 {
		c.diskUsed = 0
	}
	c.mu.Unlock()
}

func (c *Controller) createTempFile() (*os.File, string, error) {
	if c == nil {
		return nil, "", ErrStoreCapacity
	}
	c.tempOnce.Do(func() {
		c.tempDir = c.config.TempDir
		if c.tempDir == "" {
			c.tempDir = filepath.Join(os.TempDir(), "dp-body-store")
		}
		c.tempErr = os.MkdirAll(c.tempDir, 0o700)
	})
	if c.tempErr != nil {
		return nil, "", c.tempErr
	}
	file, err := os.CreateTemp(c.tempDir, "body-*")
	if err != nil {
		return nil, "", err
	}
	path := file.Name()
	if removeErr := os.Remove(path); removeErr == nil {
		path = ""
	}
	return file, path, nil
}
