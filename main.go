package main

import (
	"bytes"
	"sync"

	// "encoding/binary"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	totalStreamedSize int
)

const (
	// Sample rate of the audio file
	sampleRate = 44100
	seconds    = 1

	// Higher buffer size = more cpu intensive, but less chance for dropped data
	BUFFERSIZE = 8192
	// Lower delay = more responsive, faster streaming
	// Too high delay = dropped buffer chunks
	DELAY = 250 // milliseconds
)

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

func main() {
	filename := flag.String("filename", "./music/bou-closer-ft-slay.aac", "path to the audio file")
	flag.Parse()

	f, err := os.Open(*filename)
	if err != nil {
		log.Fatal(err)
	}

	// contents is a byte slice

	stat, err := f.Stat()
	if err != nil {
		log.Fatal("Couldn't get file stats")
	}
	log.Printf("File size: %v\n", stat.Size())

	contents, err := io.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}

	totalStreamedSize = 0
	connPool := NewConnectionPool()

	log.Println("calling go stream...")
	go stream(connPool, contents)
	// Array equal to sample rate * 1s

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/aac")
		w.Header().Set("Connection", "keep-alive")
		// w.Header().Set("Transfer-Encoding", "chunked")

		flusher, ok := w.(http.Flusher)
		if !ok {
			panic("expected http.ResponseWriter to be an http.Flusher")
		}

		connection := &Connection{bufferChannel: make(chan []byte), buffer: make([]byte, BUFFERSIZE)}
		connPool.AddConnection(connection)
		log.Printf("%s has connected to the audio stream\n", r.Host)

		for {
			buf := <-connection.bufferChannel
			if _, err := w.Write(buf); err != nil {
				connPool.DeleteConnection(connection)
				log.Printf("%s's connection to the audio stream has been closed\n", r.Host)
				return
			}
			flusher.Flush() // Triger "chunked" encoding
			log.Println("emptying buffer")
			clear(connection.buffer)
		}
	})
	log.Println("Listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
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
func stream(connectionPool *ConnectionPool, content []byte) {

	log.Println("inside go stream...")
	buffer := make([]byte, BUFFERSIZE)

	// TODO: Need to fix this and actually stop streaming when the entire file has been streamed.
	// Currently resets and resumes streaming when song has been streamed, causing file to loop indefinitely in browser.
	for {
		log.Println("inside loop iteration...")
		log.Println("buffer size:", len(buffer))
		tempfile := bytes.NewReader(content)
		clear(buffer)

		ticker := time.NewTicker(time.Millisecond * DELAY)
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
