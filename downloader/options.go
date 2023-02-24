package downloader

type DownloadTaskOptions struct {
	links         []*Link
	size          int64
	minBlockSize  int
	maxBlockSize  int
	maxWorkers    int
	eventHandler  *EventHandler
	progressStore RandomReadWriter
	dataStore     RandomReadWriter
}

func NewDefaultDownloadTaskOptions() *DownloadTaskOptions {
	return &DownloadTaskOptions{}
}

func (o *DownloadTaskOptions) AddLink(link *Link) *DownloadTaskOptions {
	o.links = append(o.links, link)
	o.maxWorkers += link.maxWorkers
	return o
}

func (o *DownloadTaskOptions) SetSize(size int64) *DownloadTaskOptions {
	o.size = size
	return o
}

func (o *DownloadTaskOptions) SetMinBlockSize(minBlockSize int) *DownloadTaskOptions {
	o.minBlockSize = minBlockSize
	return o
}

func (o *DownloadTaskOptions) SetMaxBlockSize(maxBlockSize int) *DownloadTaskOptions {
	o.maxBlockSize = maxBlockSize
	return o
}

func (o *DownloadTaskOptions) SetEventHandler(eventHandler *EventHandler) *DownloadTaskOptions {
	o.eventHandler = eventHandler
	return o
}

func (o *DownloadTaskOptions) SetProgressStore(progressStore RandomReadWriter) *DownloadTaskOptions {
	o.progressStore = progressStore
	return o
}

func (o *DownloadTaskOptions) SetDataStore(dataStore RandomReadWriter) *DownloadTaskOptions {
	o.dataStore = dataStore
	return o
}
