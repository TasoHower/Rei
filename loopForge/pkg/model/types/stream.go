package types

import "io"

// MessageStreamReader is a minimal pull-based stream of assistant messages (aligned with Eino Stream/Recv style).
// Recv returns io.EOF when the stream completes successfully. A non-nil error aborts the stream.
type MessageStreamReader interface {
	Recv() (*Message, error)
}

type sliceStreamReader struct {
	items []*Message
	i     int
}

// NewSliceStreamReader yields each message once in order, then io.EOF.
func NewSliceStreamReader(items []*Message) MessageStreamReader {
	return &sliceStreamReader{items: items, i: 0}
}

func (s *sliceStreamReader) Recv() (*Message, error) {
	if s.i >= len(s.items) {
		return nil, io.EOF
	}
	m := s.items[s.i]
	s.i++
	return m, nil
}
