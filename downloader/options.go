package downloader

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"
)

type DownloadTaskOptionsProvider func() *downloadTaskOptions

type downloadTaskOptions struct {
	links                []*Link
	size                 int64
	minBlockSize         int
	maxBlockSize         int
	maxWorkers           int
	eventHandler         *EventHandler
	progressStore        RandomReadWriter
	dataStore            RandomReadWriter
	httpClient           *http.Client
	statusUpdateInterval time.Duration
}

type DownloadTaskOptionsBuilder struct {
	*downloadTaskOptions
}

var defaultEventHandler = &EventHandler{
	OnStart: func(task *DownloadTask) {

	},
	OnFinal: func(task *DownloadTask, err error) {

	},
	OnPanic: func(task *DownloadTask, err any) {

	},
}

func NewDownloadTaskOptionsBuilder() *DownloadTaskOptionsBuilder {
	return &DownloadTaskOptionsBuilder{
		&downloadTaskOptions{
			httpClient:   http.DefaultClient,
			minBlockSize: 65535,
			maxBlockSize: math.MaxInt,
			maxWorkers:   3,
		},
	}
}

func (o *DownloadTaskOptionsBuilder) SetClient(c *http.Client) {
	o.httpClient = c
}

func (o *DownloadTaskOptionsBuilder) AddLink(link *Link) {
	o.links = append(o.links, link)
	o.maxWorkers += link.maxWorkers
}

func (o *DownloadTaskOptionsBuilder) SetSize(size int64) {
	o.size = size
}

func (o *DownloadTaskOptionsBuilder) SetMinBlockSize(minBlockSize int) {
	o.minBlockSize = minBlockSize
}

func (o *DownloadTaskOptionsBuilder) SetMaxBlockSize(maxBlockSize int) {
	o.maxBlockSize = maxBlockSize
}

func (o *DownloadTaskOptionsBuilder) SetEventHandler(eventHandler EventHandler) {
	o.eventHandler = &eventHandler
}

func (o *DownloadTaskOptionsBuilder) SetProgressStore(progressStore RandomReadWriter) {
	o.progressStore = progressStore
}

func (o *DownloadTaskOptionsBuilder) SetProgressStoreByPath(file string) (err error) {
	o.progressStore, err = os.OpenFile(file, os.O_CREATE|os.O_RDWR, os.ModePerm)
	return
}

func (o *DownloadTaskOptionsBuilder) SetDataStore(dataStore RandomReadWriter) {
	o.dataStore = dataStore
}

func (o *DownloadTaskOptionsBuilder) SetDataStoreByPath(file string) (err error) {
	o.dataStore, err = os.OpenFile(file, os.O_CREATE|os.O_RDWR, os.ModePerm)
	return
}

func (o *DownloadTaskOptionsBuilder) SetHttpClient(httpClient *http.Client) {
	o.httpClient = httpClient
}

func (o *DownloadTaskOptionsBuilder) SetStatusUpdateInterval(duration time.Duration) {
	o.statusUpdateInterval = duration
}

func (o *DownloadTaskOptionsBuilder) Build() (DownloadTaskOptionsProvider, error) {
	if len(o.links) <= 0 {
		return nil, fmt.Errorf("at least one download link needs to be provided: %w", InvalidDownloadOptions)
	} else {
		for _, v := range o.links {
			if _, err := url.Parse(v.downloadLink); err != nil {
				return nil, err
			}
			if v.maxWorkers <= 0 {
				return nil, fmt.Errorf("the maximum number of threads to download the link must be greater than zero: %w", InvalidDownloadOptions)
			}
		}
	}
	if o.size < 0 {
		return nil, fmt.Errorf("download option size must be greater than or equal to 0: %w", InvalidDownloadOptions)
	}
	if o.minBlockSize <= 0 {
		return nil, fmt.Errorf("download option minBlockSize must be greater than 0: %w", InvalidDownloadOptions)
	}
	if o.maxBlockSize <= 0 {
		return nil, fmt.Errorf("download option maxBlockSize must be greater than 0: %w", InvalidDownloadOptions)
	}
	if o.minBlockSize > o.maxBlockSize {
		return nil, fmt.Errorf("download option maxBlockSize must be greater than minBlockSize: %w", InvalidDownloadOptions)
	}
	if o.progressStore == nil || o.dataStore == nil {
		return nil, fmt.Errorf("download option progressStore (call SetProgressStore or SetProgressStoreByPath) and dataStore (call SetDataStore or SetDataStoreByPath) must provide: %w", InvalidDownloadOptions)
	}
	options := &downloadTaskOptions{
		links:                o.links,
		size:                 o.size,
		minBlockSize:         o.minBlockSize,
		maxBlockSize:         o.maxBlockSize,
		maxWorkers:           o.maxWorkers,
		eventHandler:         o.eventHandler,
		progressStore:        o.progressStore,
		dataStore:            o.dataStore,
		httpClient:           o.httpClient,
		statusUpdateInterval: o.statusUpdateInterval,
	}
	if options.eventHandler == nil {
		options.eventHandler = defaultEventHandler
	}
	if options.statusUpdateInterval <= 0 {
		options.statusUpdateInterval = 1 * time.Second
	}
	if options.httpClient == nil {
		options.httpClient = http.DefaultClient
	}
	return func() *downloadTaskOptions {
		return options
	}, nil
}
