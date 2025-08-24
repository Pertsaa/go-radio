package radio

import "sync"

type RingBuffer struct {
	data  [][]byte
	head  int
	size  int
	mutex sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([][]byte, size),
		size: size,
	}
}

func (b *RingBuffer) Write(chunk []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.data[b.head] = chunk
	b.head = (b.head + 1) % b.size
}

func (b *RingBuffer) ReadAll() [][]byte {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	result := make([][]byte, 0, b.size)
	for i := b.head; i < b.size; i++ {
		if b.data[i] != nil {
			result = append(result, b.data[i])
		}
	}
	for i := 0; i < b.head; i++ {
		if b.data[i] != nil {
			result = append(result, b.data[i])
		}
	}
	return result
}
