package mutex

import "sync"

func WithLock[T any](mutex *sync.Mutex, fun func() T) T {
	mutex.Lock()
	defer mutex.Unlock()
	return fun()
}
