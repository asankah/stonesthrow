package stonesthrow

import (
	"io"
	"net"
	"time"
)

type PipeConnection struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func (s PipeConnection) Read(b []byte) (int, error) {
	return s.reader.Read(b)
}

func (s PipeConnection) Write(b []byte) (int, error) {
	return s.writer.Write(b)
}

func (s PipeConnection) Close() error {
	s.reader.Close()
	return s.writer.Close()
}

func (s PipeConnection) LocalAddr() net.Addr {
	return nil
}

func (s PipeConnection) RemoteAddr() net.Addr {
	return nil
}

func (s PipeConnection) SetDeadline(t time.Time) error {
	return nil
}

func (s PipeConnection) SetReadDeadline(t time.Time) error {
	return nil
}

func (s PipeConnection) SetWriteDeadline(t time.Time) error {
	return nil
}
