package downloader

import (
	"fmt"
	"strconv"
	"strings"
)

func ResolveContentRange(contentRange string) (int64, error) {
	if contentRange != "" {
		r := strings.Split(contentRange, "/")
		if len(r) != 2 {
			return -1, fmt.Errorf("invalid content range \"%s\"", contentRange)
		}
		contentLength, err := strconv.ParseInt(r[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("error parsing content length from \"%s\": %w", r[1], err)
		}
		return contentLength, nil
	}
	return 0, fmt.Errorf("content-length not exist in response header")
}
