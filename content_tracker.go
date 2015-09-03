package wbgo

import (
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"sync"
)

type ContentTracker struct {
	sync.Mutex
	hashes map[string]string
}

func NewContentTracker() *ContentTracker {
	return &ContentTracker{hashes: make(map[string]string)}
}

func (tracker *ContentTracker) Track(key, path string) (bool, error) {
	tracker.Lock()
	defer tracker.Unlock()

	bs, err := ioutil.ReadFile(path)
	if err != nil {
		return false, err
	}

	h := sha1.New()
	hash := fmt.Sprintf("%x", h.Sum(bs))
	oldHash := tracker.hashes[key]
	if oldHash != hash {
		tracker.hashes[key] = hash
		return true, nil
	}

	return false, nil
}
