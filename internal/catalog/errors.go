package catalog

import "errors"

var (
	ErrSourceNotFound = errors.New("catalog: source not found")
	ErrRunNotRunning  = errors.New("catalog: sync run is not running")
)
