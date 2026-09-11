package proto

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func valid() Header {
	return Header{Magic: Magic, Op: OpGet, PageShift: PageShift4K, Seq: 7, Page: 42}
}

func encode(h Header) []byte {
	b := make([]byte, HeaderSize)
	h.Encode(b)
	return b
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		h    Header
	}{
		{"ping", Header{Magic: Magic, Op: OpPing, PageShift: PageShift4K, Seq: 1}},
		{"pong", Header{Magic: Magic, Op: OpPong, PageShift: PageShift4K, Seq: 1}},
		{"get", Header{Magic: Magic, Op: OpGet, PageShift: PageShift4K, Seq: 2, Page: 42}},
		{"getr", Header{Magic: Magic, Op: OpGetR, PageShift: PageShift4K, Seq: 2, Page: 42, Len: 4096}},
		{"getr err", Header{Magic: Magic, Op: OpGetR, Flags: FlagErr, PageShift: PageShift4K, Seq: 3, Page: 1 << 40}},
		{"put", Header{Magic: Magic, Op: OpPut, PageShift: PageShift4K, Seq: 4, Page: 7, Len: 4096}},
		{"puta", Header{Magic: Magic, Op: OpPutA, PageShift: PageShift4K, Seq: 4, Page: 7}},
		{"max fields", Header{Magic: Magic, Op: OpPutA, Flags: 0xff, PageShift: 0xff,
			Seq: math.MaxUint64, Page: math.MaxUint64, Len: math.MaxUint32}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(encode(tt.h))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			got.CRC = 0 // Encode ignores the input CRC; Decode already verified it
			if got != tt.h {
				t.Fatalf("got %+v, want %+v", got, tt.h)
			}
		})
	}
}

// TestGolden locks the wire layout, byte order and CRC algorithm.
func TestGolden(t *testing.T) {
	h := Header{Magic: Magic, Op: OpGetR, Flags: FlagErr, PageShift: PageShift4K,
		Seq: 0x0102030405060708, Page: 0x1112131415161718, Len: 4096}
	want := []byte{
		'f', 'm', 'e', 'm', // magic
		0x04, 0x01, 0x0c, 0x00, // op, flags, pageshift, rsvd
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, // seq
		0x18, 0x17, 0x16, 0x15, 0x14, 0x13, 0x12, 0x11, // page
		0x00, 0x10, 0x00, 0x00, // len
		0xb1, 0xc3, 0x09, 0x4a, // crc32c
	}
	if got := encode(h); !bytes.Equal(got, want) {
		t.Fatalf("got  % x\nwant % x", got, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	withOp := func(op Op) []byte { h := valid(); h.Op = op; return encode(h) }
	flip := func(i int) []byte { b := encode(valid()); b[i] ^= 1; return b }
	rsvd := valid()
	rsvd.Rsvd = 1

	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrShort},
		{"short", encode(valid())[:HeaderSize-1], ErrShort},
		{"bad magic", flip(0), ErrMagic},
		{"corrupt seq", flip(9), ErrCRC},
		{"corrupt crc", flip(31), ErrCRC},
		{"op zero", withOp(0), ErrOp},
		{"op past end", withOp(OpPutA + 1), ErrOp},
		{"rsvd set", encode(rsvd), ErrRsvd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDecodeIgnoresTrailingPayload(t *testing.T) {
	b := append(encode(valid()), make([]byte, 4096)...)
	if _, err := Decode(b); err != nil {
		t.Fatalf("Decode: %v", err)
	}
}

func TestOpString(t *testing.T) {
	tests := []struct {
		op   Op
		want string
	}{
		{OpPing, "PING"}, {OpPong, "PONG"}, {OpGet, "GET"},
		{OpGetR, "GETR"}, {OpPut, "PUT"}, {OpPutA, "PUTA"},
		{0, "Op(0)"}, {200, "Op(200)"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("Op(%d).String() = %q, want %q", uint8(tt.op), got, tt.want)
		}
	}
}

func TestNoAllocs(t *testing.T) {
	var b [HeaderSize]byte
	h := valid()
	n := testing.AllocsPerRun(100, func() {
		h.Encode(b[:])
		if _, err := Decode(b[:]); err != nil {
			t.Fatal(err)
		}
	})
	if n != 0 {
		t.Fatalf("Encode+Decode allocated %v times per run, want 0", n)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(encode(valid()))
	f.Add(encode(Header{Magic: Magic, Op: OpPut, PageShift: PageShift4K, Len: 4096}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, in []byte) {
		h, err := Decode(in)
		if err != nil {
			return
		}
		// Anything Decode accepts must re-encode to the same bytes.
		if out := encode(h); !bytes.Equal(out, in[:HeaderSize]) {
			t.Fatalf("re-encode mismatch:\n in % x\nout % x", in[:HeaderSize], out)
		}
	})
}
