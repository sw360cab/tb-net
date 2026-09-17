// Package proto defines the ultramem wire protocol: a fixed 32-byte,
// little-endian header, optionally followed by a page payload.
package proto

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"strconv"
)

// HeaderSize is the encoded size of a Header on the wire.
const HeaderSize = 32

// Magic identifies an ultramem frame. Little-endian on the wire it reads "umem".
const Magic uint32 = 0x6d656d75

// PageShift4K is the page shift for 4 KB pages, the only size used today.
const PageShift4K = 12

// FlagErr marks a reply whose request failed (e.g. page out of range).
const FlagErr uint8 = 1 << 0

// Op is the operation carried by a frame.
type Op uint8

// Ops come in request/reply pairs. Zero is reserved so an uninitialized
// header never decodes as a valid op. Append only: position is wire value.
const (
	OpPing Op = iota + 1 // liveness/RTT probe, no payload
	OpPong               // reply to OpPing, no payload
	OpGet                // request page, no payload
	OpGetR               // reply to OpGet, carries the page
	OpPut                // store page, carries the page
	OpPutA               // ack for OpPut, no payload
)

var opNames = [...]string{
	OpPing: "PING",
	OpPong: "PONG",
	OpGet:  "GET",
	OpGetR: "GETR",
	OpPut:  "PUT",
	OpPutA: "PUTA",
}

// Valid reports whether o is a defined op.
func (o Op) Valid() bool { return o != 0 && int(o) < len(opNames) }

// String returns the op's protocol name, or Op(n) for unknown values.
func (o Op) String() string {
	if o.Valid() {
		return opNames[o]
	}
	return "Op(" + strconv.Itoa(int(o)) + ")"
}

// Header is the fixed-size frame header (CLAUDE.md §3).
//
//	magic u32 | op u8 | flags u8 | pageshift u8 | rsvd u8 | seq u64 | page u64 | len u32 | crc u32
//
// Field order matches the wire layout; encoding is explicit, never via
// in-memory layout.
type Header struct {
	Magic     uint32 // protocol identifier, always Magic
	Op        Op     // one of the Op* constants
	Flags     uint8  // FlagErr, future batch bits
	PageShift uint8  // log2(page size); PageShift4K for now
	Rsvd      uint8  // must be zero
	Seq       uint64 // request id, echoed in the reply
	Page      uint64 // page index in the memsrv buffer
	Len       uint32 // payload bytes following the header
	CRC       uint32 // CRC-32C over bytes 0..27 of the encoded header
}

// Decode errors.
var (
	ErrShort = errors.New("proto: short header")
	ErrMagic = errors.New("proto: bad magic")
	ErrCRC   = errors.New("proto: header crc mismatch")
	ErrOp    = errors.New("proto: unknown op")
	ErrRsvd  = errors.New("proto: reserved byte not zero")
)

const crcOff = 28

var castagnoliCrc = crc32.MakeTable(crc32.Castagnoli)

// Encode writes h into b[:HeaderSize] and fills in the CRC; h.CRC is ignored.
// It panics if len(b) < HeaderSize.
func (h Header) Encode(b []byte) {
	_ = b[HeaderSize-1]
	binary.LittleEndian.PutUint32(b[0:], h.Magic)
	b[4] = byte(h.Op)
	b[5] = h.Flags
	b[6] = h.PageShift
	b[7] = h.Rsvd
	binary.LittleEndian.PutUint64(b[8:], h.Seq)
	binary.LittleEndian.PutUint64(b[16:], h.Page)
	binary.LittleEndian.PutUint32(b[24:], h.Len)
	binary.LittleEndian.PutUint32(b[crcOff:], crc32.Checksum(b[:crcOff], castagnoliCrc))
}

// Decode parses and validates the header in b[:HeaderSize]: magic, CRC, op,
// reserved byte. Payload length rules are left to the caller, since they
// depend on op and flags.
func Decode(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrShort
	}
	h := Header{
		Magic:     binary.LittleEndian.Uint32(b[0:]),
		Op:        Op(b[4]),
		Flags:     b[5],
		PageShift: b[6],
		Rsvd:      b[7],
		Seq:       binary.LittleEndian.Uint64(b[8:]),
		Page:      binary.LittleEndian.Uint64(b[16:]),
		Len:       binary.LittleEndian.Uint32(b[24:]),
		CRC:       binary.LittleEndian.Uint32(b[crcOff:]),
	}
	switch {
	case h.Magic != Magic:
		return Header{}, ErrMagic
	case crc32.Checksum(b[:crcOff], castagnoliCrc) != h.CRC:
		return Header{}, ErrCRC
	case !h.Op.Valid():
		return Header{}, ErrOp
	case h.Rsvd != 0:
		return Header{}, ErrRsvd
	}
	return h, nil
}
