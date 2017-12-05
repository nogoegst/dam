// dam.go - simple periodically purgeable cache
//
// To the extent possible under law, Ivan Markin waived all copyright
// and related or neighboring rights to this module of dam, using the creative
// commons "CC0" public domain dedication. See LICENSE or
// <http://creativecommons.org/publicdomain/zero/1.0/> for full details.

package dam

import (
	"errors"
	"sync"
	"time"

	"golang.org/x/crypto/blake2b"
)

var (
	ErrNotFound = errors.New("not found")
)

const (
	NoPurge = time.Duration(0)
)

// Marshallable represents a struct (typicaly a profobuf struct)
// that can be serialized into byte slice.
type Marshallable interface {
	Marshal() ([]byte, error)
}

// Dam represents instance of purgeable cache.
type Dam struct {
	mutex   sync.RWMutex
	storage map[string]interface{}

	ticker     *time.Ticker
	tickerDone chan struct{}
}

// New creates a Dam that purges every duration.
// If set to NoPurge or value less than zero
// the Dam will never purge.
func New(duration time.Duration) *Dam {
	d := &Dam{
		storage:    make(map[string]interface{}),
		tickerDone: make(chan struct{}),
	}
	if duration > time.Duration(0) {
		d.ticker = time.NewTicker(duration)
		go func() {
			for {
				select {
				case <-d.ticker.C:
					d.Purge()
				case <-d.tickerDone:
					return
				}
			}
		}()
	}
	return d
}

func hash(s Marshallable) (string, error) {
	m, err := s.Marshal()
	if err != nil {
		return "", err
	}
	h, err := blake2b.New256(nil)
	if err != nil {
		panic(err)
	}
	h.Write(m)
	ret := string(m[:8])
	return ret, nil
}

// Store sets the value for a key.
func (d *Dam) Store(key Marshallable, value interface{}) error {
	k, err := hash(key)
	if err != nil {
		return err
	}
	d.mutex.Lock()
	d.storage[k] = value
	d.mutex.Unlock()
	return nil
}

// Load returns existing value stored for the key.
// If no value is present it returns ErrNotFound as the error.
func (d *Dam) Load(key Marshallable) (interface{}, error) {
	k, err := hash(key)
	if err != nil {
		return nil, err
	}
	d.mutex.RLock()
	value, ok := d.storage[k]
	d.mutex.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return value, nil
}

// FetchFunc represents a function that fetches value to be
// stored in Dam.
type FetchFunc func() (interface{}, error)

// LoadOrStore returns existing value for the key if present.
// If the is no value it will call fetch function and set given value
// for the key.
// Note: fetch function is supposed to be called as a closure and
// fetch value for the key.
func (d *Dam) LoadOrStore(key Marshallable, fetch FetchFunc) (interface{}, error) {
	v, err := d.Load(key)
	if err == ErrNotFound {
		v, err = fetch()
		if err != nil {
			return nil, err
		}
		err = d.Store(key, v)
	}
	return v, err
}

// Range ranges over existing entries in a Dam. Range does not
// represent snapshot of Dam. Range returns if f returns false.
func (d *Dam) Range(f func(value interface{}) bool) {
	var keys []string
	d.mutex.RLock()
	for k, _ := range d.storage {
		keys = append(keys, k)
	}
	d.mutex.RUnlock()

	for _, k := range keys {
		d.mutex.RLock()
		value, ok := d.storage[k]
		d.mutex.RUnlock()
		if !ok {
			continue
		}
		if !f(value) {
			return
		}
	}
}

// Purge purges Dam.
func (d *Dam) Purge() {
	d.mutex.Lock()
	d.storage = make(map[string]interface{})
	d.mutex.Unlock()
}

// Stop stops purging of the Dam and allows underlying
// resources to be freed.
func (d *Dam) Stop() {
	if d.ticker == nil {
		return
	}
	d.ticker.Stop()
	close(d.tickerDone)
}
