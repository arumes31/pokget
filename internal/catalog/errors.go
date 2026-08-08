package catalog

import "errors"

var (
	ErrSourceNotFound      = errors.New("catalog: source not found")
	ErrRunNotRunning       = errors.New("catalog: sync run is not running")
	ErrEmptySnapshot       = errors.New("catalog: complete snapshot contains no records")
	ErrRecordCountMismatch = errors.New("catalog: source record count mismatch")
)
