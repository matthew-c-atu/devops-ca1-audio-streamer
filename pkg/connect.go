package connect

import (
	"bytes"
	"io"
	"log"
	"sync"
	"time"
)

var (
	totalStreamedSize int
)

// The settings to pass to the creation of a new connection
type AudioSettings struct {
	SampleRate int
	seconds    int
	BufferSize int
	Delay      int
}

// Wrapper for what is required with each connection - a byte slice channel buffer and a byte slice buffer

type Connection struct {
	bufferChannel chan []byte
	buffer        []byte
}

// Need a way to handle multiple requests concurrently - this means connection doesn't get blocked

// Trying to do this without concurrency results in the stream crashing after loading the first buffered chunk
// ConnectionPool is a singleton
type ConnectionPool struct {
	// Map pointer to connection to empty struct
	ConnectionMap map[*Connection]struct{}
	// Mutex to prevent data races when handling concurrent requests
	mu sync.Mutex
}

// Add connection without blocking
func (cp *ConnectionPool) AddConnection(connection *Connection) {
	defer cp.mu.Unlock()
	cp.mu.Lock()
	cp.ConnectionMap[connection] = struct{}{}
}

// Delete connection without blocking
func (cp *ConnectionPool) DeleteConnection(connection *Connection) {
	defer cp.mu.Unlock()
	cp.mu.Lock()
	delete(cp.ConnectionMap, connection)
}

func NewConnectionPool() *ConnectionPool {
	connectionMap := make(map[*Connection]struct{})
	return &ConnectionPool{ConnectionMap: connectionMap}
}

func (cp *ConnectionPool) Broadcast(buffer []byte) {
	// first, make sure cp won't data race...
	defer cp.mu.Unlock()
	cp.mu.Lock()

	for connection := range cp.ConnectionMap {
		copy(connection.buffer, buffer)
		// Waits until each individual connection.bufferChannel is free
		select {
		case connection.bufferChannel <- connection.buffer:
			size := len(connection.buffer)
			totalStreamedSize += size
			// log.Printf("Total streamed size: %v", totalStreamedSize)
		default:
		}
	}
}

// Reads from entire contents of file and broadcasts to each connection in the connectionpool
func stream(connectionPool *ConnectionPool, content []byte, settings *AudioSettings) {

	log.Println("inside go stream...")
	buffer := make([]byte, settings.BufferSize)

	// TODO: Need to fix this and actually stop streaming when the entire file has been streamed.
	// Currently resets and resumes streaming when song has been streamed, causing file to loop indefinitely in browser.
	for {
		log.Println("inside loop iteration...")
		log.Println("buffer size:", len(buffer))
		tempfile := bytes.NewReader(content)
		clear(buffer)

		ticker := time.NewTicker(time.Millisecond * time.Duration(settings.Delay))
		// Changing ticker delay causes below code to be executed every DELAY ms
		for range ticker.C {
			// log.Println("inside ticker iteration...")
			// read INTO buffer
			_, err := tempfile.Read(buffer)
			if err == io.EOF {
				log.Println("Whole file streamed")
				ticker.Stop()
				break
			}
			connectionPool.Broadcast(buffer)
		}
	}
}
