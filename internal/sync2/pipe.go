// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information

package sync2

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"sync"
	"sync/atomic"
)

// pipe is a io.Reader/io.Writer pipe backed by ReadAtWriteAtCloser
type pipe struct {
	buffer ReadAtWriteAtCloser
	open   int32 // number of halves open (starts at 2)

	mu     sync.Mutex
	nodata sync.Cond
	read   int64
	write  int64
	limit  int64

	writerDone bool
	writerErr  error

	readerDone bool
	readerErr  error
}

// NewPipeFile returns a pipe that uses file-system to offload memory
func NewPipeFile(tempdir string) (PipeReader, PipeWriter, error) {
	tempfile, err := ioutil.TempFile(tempdir, "filepipe")
	if err != nil {
		return nil, nil, err
	}

	pipe := &pipe{
		buffer: tempfile,
		open:   2,
		limit:  math.MaxInt64,
	}
	pipe.nodata.L = &pipe.mu

	return pipeReader{pipe}, pipeWriter{pipe}, nil
}

// NewPipeMemory returns a pipe that uses an inmemory buffer
func NewPipeMemory(pipeSize int64) (PipeReader, PipeWriter, error) {
	pipe := &pipe{
		buffer: make(memory, pipeSize),
		open:   2,
		limit:  pipeSize,
	}
	pipe.nodata.L = &pipe.mu
	return pipeReader{pipe}, pipeWriter{pipe}, nil
}

type pipeReader struct{ pipe *pipe }
type pipeWriter struct{ pipe *pipe }

// Close implements io.Reader Close
func (reader pipeReader) Close() error { return reader.CloseWithError(io.ErrClosedPipe) }

// Close implements io.Writer Close
func (writer pipeWriter) Close() error { return writer.CloseWithError(io.EOF) }

// CloseWithError implements closing with error
func (reader pipeReader) CloseWithError(err error) error {
	pipe := reader.pipe
	pipe.mu.Lock()
	if pipe.readerDone {
		pipe.mu.Unlock()
		return io.ErrClosedPipe
	}
	pipe.readerDone = true
	pipe.readerErr = err
	pipe.mu.Unlock()

	return pipe.closeHalf()
}

// CloseWithError implements closing with error
func (writer pipeWriter) CloseWithError(err error) error {
	pipe := writer.pipe
	pipe.mu.Lock()
	if pipe.writerDone {
		pipe.mu.Unlock()
		return io.ErrClosedPipe
	}
	pipe.writerDone = true
	pipe.writerErr = err
	pipe.nodata.Broadcast()
	pipe.mu.Unlock()

	return pipe.closeHalf()
}

// closeHalf closes one side of the pipe
func (pipe *pipe) closeHalf() error {
	if atomic.AddInt32(&pipe.open, -1) == 0 {
		return pipe.buffer.Close()
	}
	return nil
}

// Write writes to the pipe returning io.ErrClosedPipe when pipeSize is reached
func (writer pipeWriter) Write(data []byte) (n int, err error) {
	pipe := writer.pipe
	pipe.mu.Lock()

	// has the reader finished?
	if pipe.readerDone {
		pipe.mu.Unlock()
		return 0, pipe.readerErr
	}

	// have we closed already
	if pipe.writerDone {
		pipe.mu.Unlock()
		return 0, io.ErrClosedPipe
	}

	// check how much do they want to write
	canWrite := pipe.limit - pipe.write

	// no more room to write
	if canWrite == 0 {
		pipe.mu.Unlock()
		return 0, io.ErrClosedPipe
	}

	// figure out how much to write
	toWrite := int64(len(data))
	if toWrite > canWrite {
		toWrite = canWrite
	}

	writeAt := pipe.write
	pipe.mu.Unlock()

	// write data to buffer
	writeAmount, err := pipe.buffer.WriteAt(data[:toWrite], writeAt)

	pipe.mu.Lock()
	// update writing head
	pipe.write += int64(writeAmount)
	// wake up reader
	pipe.nodata.Broadcast()
	// check whether we have finished
	done := pipe.write >= pipe.limit
	pipe.mu.Unlock()

	if err == nil && done {
		err = io.ErrClosedPipe
	}
	return writeAmount, err
}

// Read reads from the pipe returning io.EOF when writer is closed or pipeSize is reached
func (reader pipeReader) Read(data []byte) (n int, err error) {
	pipe := reader.pipe
	pipe.mu.Lock()
	// wait until we have something to read
	for pipe.read >= pipe.write {
		// has the writer finished?
		if pipe.writerDone {
			pipe.mu.Unlock()
			return 0, pipe.writerErr
		}

		// have we closed already
		if pipe.readerDone {
			pipe.mu.Unlock()
			return 0, io.ErrClosedPipe
		}

		// have we run out of the limit
		if pipe.read >= pipe.limit {
			pipe.mu.Unlock()
			return 0, io.EOF
		}

		// ok, lets wait
		pipe.nodata.Wait()
	}

	// how much there's available for reading
	canRead := pipe.write - pipe.read
	// how much do they want to read?
	toRead := int64(len(data))
	if toRead > canRead {
		toRead = canRead
	}
	readAt := pipe.read
	pipe.mu.Unlock()

	// read data
	readAmount, err := pipe.buffer.ReadAt(data[:toRead], readAt)

	pipe.mu.Lock()
	// update info on how much we have read
	pipe.read += int64(readAmount)
	done := pipe.read >= pipe.limit
	pipe.mu.Unlock()

	if err == nil && done {
		err = io.EOF
	}
	return readAmount, err
}

// MultiPipe is a multipipe backed by a single file
type MultiPipe struct {
	pipes []pipe
	open  int64 // number of pipes open
}

// NewMultiPipeFile returns a new MultiPipe that is created in tempdir
// if tempdir == "" the fill will be created it into os.TempDir
func NewMultiPipeFile(tempdir string, pipeCount, pipeSize int64) (*MultiPipe, error) {
	tempfile, err := ioutil.TempFile(tempdir, "multifilepipe")
	if err != nil {
		return nil, err
	}

	err = tempfile.Truncate(pipeCount * pipeSize)
	if err != nil {
		closeErr := tempfile.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("%v/%v", err, closeErr)
		}
		return nil, err
	}

	multipipe := &MultiPipe{
		pipes: make([]pipe, pipeCount),
		open:  pipeCount,
	}

	for i := range multipipe.pipes {
		pipe := &multipipe.pipes[i]
		pipe.buffer = offsetFile{
			file:   tempfile,
			offset: int64(i) * pipeSize,
			open:   &multipipe.open,
		}
		pipe.limit = pipeSize
		pipe.nodata.L = &pipe.mu
	}

	return multipipe, nil
}

// NewMultiPipeMemory returns a new MultiPipe that is using a memory buffer
func NewMultiPipeMemory(pipeCount, pipeSize int64) (*MultiPipe, error) {
	buffer := make(memory, pipeCount*pipeSize)

	multipipe := &MultiPipe{
		pipes: make([]pipe, pipeCount),
		open:  pipeCount,
	}

	for i := range multipipe.pipes {
		pipe := &multipipe.pipes[i]
		pipe.buffer = buffer[i*int(pipeSize) : (i+1)*int(pipeSize)]
		pipe.limit = pipeSize
		pipe.nodata.L = &pipe.mu
	}

	return multipipe, nil
}

// Pipe returns the two ends of a block stream pipe
func (multipipe *MultiPipe) Pipe(index int) (PipeReader, PipeWriter) {
	pipe := &multipipe.pipes[index]
	return pipeReader{pipe}, pipeWriter{pipe}
}
