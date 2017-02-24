package smux

import (
	"encoding/binary"
	"fmt"
)

const ( // cmds
	cmdSYN byte = iota // stream open
	cmdFIN             // stream close, a.k.a EOF mark
	cmdPSH             // data push
	cmdNOP             // no operation
)

const (
	sizeOfCmd    = 1
	sizeOfLength = 2
	sizeOfSid    = 4
	headerSize   = sizeOfCmd + sizeOfSid + sizeOfLength
)

// Frame defines a packet from or to be multiplexed into a single connection
type Frame struct {
	cmd  byte
	sid  uint32
	data []byte
}

func newFrame(cmd byte, sid uint32) Frame {
	return Frame{cmd: cmd, sid: sid}
}

type rawHeader []byte

func (h rawHeader) Cmd() byte {
	return h[0]
}

func (h rawHeader) Length() uint16 {
	return binary.LittleEndian.Uint16(h[1:])
}

func (h rawHeader) StreamID() uint32 {
	return binary.LittleEndian.Uint32(h[3:])
}

func (h rawHeader) String() string {
	return fmt.Sprintf("Cmd:%d StreamID:%d Length:%d", h.Cmd(), h.StreamID(), h.Length())
}
