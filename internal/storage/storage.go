package storage

import "sync"

type mem struct {
	files struct {
		sync.RWMutex
		// data map[string]*File
	}
}
