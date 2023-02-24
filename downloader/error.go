package downloader

import "errors"

var InvalidProgressData = errors.New("invalid progress data")
var WriteAlreadyFinished = errors.New("write already finished")
var BadResponse = errors.New("bad response")
