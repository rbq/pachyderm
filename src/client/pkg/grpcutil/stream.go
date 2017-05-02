package grpcutil

import (
	"bytes"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/gogo/protobuf/types"
)

var (
	// MaxMsgSize is used to define the GRPC frame size
	MaxMsgSize = 20 * 1024 * 1024
)

// Chunk splits a piece of data up, this is useful for splitting up data that's
// bigger than MaxMsgSize
func Chunk(data []byte, chunkSize int) [][]byte {
	var result [][]byte
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		result = append(result, data[i:end])
	}
	return result
}

// ChunkReader splits a reader into chunks of size chunkSize.  For each
// chunk, it calls the given function.
func ChunkReader(r io.Reader, chunkSize int, f func([]byte) error) (int, error) {
	var total int
	for {
		buf := make([]byte, chunkSize)
		n, err := r.Read(buf)
		if n == 0 && err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
		if err := f(buf[:n]); err != nil {
			return total, err
		}
		total += n
	}
}

// StreamingBytesServer represents a server for an rpc method of the form:
//   rpc Foo(Bar) returns (stream google.protobuf.BytesValue) {}
type StreamingBytesServer interface {
	Send(bytesValue *types.BytesValue) error
}

// StreamingBytesClient represents a client for an rpc method of the form:
//   rpc Foo(Bar) returns (stream google.protobuf.BytesValue) {}
type StreamingBytesClient interface {
	Recv() (*types.BytesValue, error)
}

// NewStreamingBytesReader returns an io.Reader for a StreamingBytesClient.
func NewStreamingBytesReader(streamingBytesClient StreamingBytesClient) io.Reader {
	return &streamingBytesReader{streamingBytesClient: streamingBytesClient}
}

type streamingBytesReader struct {
	streamingBytesClient StreamingBytesClient
	buffer               bytes.Buffer
}

func (s *streamingBytesReader) Read(p []byte) (int, error) {
	// TODO this is doing an unneeded copy (unless go is smarter than I think it is)
	if s.buffer.Len() == 0 {
		value, err := s.streamingBytesClient.Recv()
		if err != nil {
			return 0, err
		}
		if _, err := s.buffer.Write(value.Value); err != nil {
			return 0, err
		}
	}
	return s.buffer.Read(p)
}

// NewStreamingBytesWriter returns an io.Writer for a StreamingBytesServer.
func NewStreamingBytesWriter(streamingBytesServer StreamingBytesServer) io.Writer {
	return &streamingBytesWriter{streamingBytesServer}
}

type streamingBytesWriter struct {
	streamingBytesServer StreamingBytesServer
}

func (s *streamingBytesWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := s.streamingBytesServer.Send(&types.BytesValue{Value: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteToStreamingBytesServer writes the data from the io.Reader to the StreamingBytesServer.
func WriteToStreamingBytesServer(reader io.Reader, streamingBytesServer StreamingBytesServer) error {
	buf := GetBuffer()
	defer PutBuffer(buf)
	_, err := io.CopyBuffer(NewStreamingBytesWriter(streamingBytesServer), reader, buf)
	return err
}

// countWriter increments a counter by the number of bytes given
type countWriter struct {
	count int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(&w.count, int64(len(p)))
	return len(p), nil
}

// WriteFromStreamingBytesClient writes from the StreamingBytesClient to the io.Writer.
func WriteFromStreamingBytesClient(streamingBytesClient StreamingBytesClient, writer io.Writer) (retErr error) {
	var total int
	defer func() {
		fmt.Printf("Wrote %d bytes in WriteFromStreamingBytesClient\n", total)
		if retErr != nil {
			fmt.Printf("error: %v\n", retErr)
		}
	}()
	for bytesValue, err := streamingBytesClient.Recv(); err != io.EOF; bytesValue, err = streamingBytesClient.Recv() {
		if err != nil {
			return err
		}
		n, err := writer.Write(bytesValue.Value)
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}
