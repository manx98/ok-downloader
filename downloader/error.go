package downloader

import "errors"

var InvalidProgressData = errors.New("invalid progress data")
var WriteAlreadyFinished = errors.New("write already finished")
var BadResponse = errors.New("bad response")
var InvalidDownloadOptions = errors.New("invalid download options")
