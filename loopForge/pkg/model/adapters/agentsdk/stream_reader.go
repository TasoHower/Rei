package agentsdk

import (
	"io"

	lpmodel "loopforge/pkg/model"
)

type streamPart struct {
	msg *lpmodel.Message
	err error
}

// chanStreamReader implements lpmodel.MessageStreamReader from a channel of parts.
type chanStreamReader struct {
	ch <-chan streamPart
}

func newChanStreamReader(ch <-chan streamPart) lpmodel.MessageStreamReader {
	return &chanStreamReader{ch: ch}
}

var _ lpmodel.MessageStreamReader = (*chanStreamReader)(nil)

func (r *chanStreamReader) Recv() (*lpmodel.Message, error) {
	item, ok := <-r.ch
	if !ok {
		return nil, io.EOF
	}
	if item.err != nil {
		return nil, item.err
	}
	return item.msg, nil
}
