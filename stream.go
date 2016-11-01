package smux

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

// Stream implements io.ReadWriteCloser
type Stream struct {
	id           uint32
	rstflag      int32
	sess         *Session
	buffer       bytes.Buffer
	bufferLock   sync.Mutex
	frameSize    int
	chReadEvent  chan struct{} // notify a read event
	die          chan struct{} // flag the stream has closed
	dieLock      sync.Mutex
	readDeadline atomic.Value
}

// newStream initiates a Stream struct
func newStream(id uint32, frameSize int, sess *Session) *Stream {
	s := new(Stream)
	s.id = id
	s.chReadEvent = make(chan struct{}, 1)
	s.frameSize = frameSize
	s.sess = sess
	s.die = make(chan struct{})
	return s
}

// Read implements io.ReadWriteCloser
func (s *Stream) Read(b []byte) (n int, err error) {
	var deadline <-chan time.Time
	if d, ok := s.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer := time.NewTimer(d.Sub(time.Now()))
		defer timer.Stop()
		deadline = timer.C
	}

READ:
	select {
	case <-s.die:
		return 0, errors.New(errBrokenPipe)
	case <-deadline:
		return n, errTimeout
	default:
	}

	s.bufferLock.Lock()
	n, err = s.buffer.Read(b)
	s.bufferLock.Unlock()

	if n > 0 {
		s.sess.returnTokens(n)
		return n, nil
	} else if atomic.LoadInt32(&s.rstflag) == 1 {
		_ = s.Close()
		return 0, errors.New(errConnReset)
	}

	select {
	case <-s.chReadEvent:
		goto READ
	case <-deadline:
		return n, errTimeout
	case <-s.die:
		return 0, errors.New(errBrokenPipe)
	}
}

// Write implements io.ReadWriteCloser
func (s *Stream) Write(b []byte) (n int, err error) {
	select {
	case <-s.die:
		return 0, errors.New(errBrokenPipe)
	default:
	}

	frames := s.split(b, cmdPSH, s.id)
	for k := range frames {
		if _, err := s.sess.writeFrame(frames[k]); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// Close implements io.ReadWriteCloser
func (s *Stream) Close() error {
	s.dieLock.Lock()
	defer s.dieLock.Unlock()

	select {
	case <-s.die:
		return errors.New(errBrokenPipe)
	default:
		close(s.die)
		s.sess.streamClosed(s.id)
		_, err := s.sess.writeFrame(newFrame(cmdRST, s.id))
		return err
	}
}

// SetReadDeadline sets the read deadline as defined by
// net.Conn.SetReadDeadline.
// A zero time value disables the deadline.
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.readDeadline.Store(t)
	return nil
}

// session closes the stream
func (s *Stream) sessionClose() {
	s.dieLock.Lock()
	defer s.dieLock.Unlock()

	select {
	case <-s.die:
	default:
		close(s.die)
	}
}

// pushBytes a slice into buffer
func (s *Stream) pushBytes(p []byte) {
	s.bufferLock.Lock()
	s.buffer.Write(p)
	s.bufferLock.Unlock()
}

// recycleTokens transform remaining bytes to tokens(will truncate buffer)
func (s *Stream) recycleTokens() (n int) {
	s.bufferLock.Lock()
	n = s.buffer.Len()
	s.buffer.Reset()
	s.bufferLock.Unlock()
	return
}

// split large byte buffer into smaller frames, reference only
func (s *Stream) split(bts []byte, cmd byte, sid uint32) []Frame {
	var frames []Frame
	for len(bts) > s.frameSize {
		frame := newFrame(cmd, sid)
		frame.data = bts[:s.frameSize]
		bts = bts[s.frameSize:]
		frames = append(frames, frame)
	}
	if len(bts) > 0 {
		frame := newFrame(cmd, sid)
		frame.data = bts
		frames = append(frames, frame)
	}
	return frames
}

// notify read event
func (s *Stream) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

// mark this stream has been reset
func (s *Stream) markRST() {
	atomic.StoreInt32(&s.rstflag, 1)
}

var errTimeout error = &timeoutError{}

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
