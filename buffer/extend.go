package buffer

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"unsafe"
)

// AppendBytes appends a byte slice to buffer.
func (b *Buffer) Write(data []byte) (n int, err error) {
	b.AppendBytes(data)
	return len(data), nil
}

// AppendBytes appends a byte slice to buffer.
func (b *Buffer) WriteString(data string) (n int, err error) {
	b.AppendString(data)
	return len(data), nil
}

// WriteTo outputs the contents of a buffer to a writer and resets the buffer.
func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	var wLen int
	for _, buf := range b.bufs {
		wLen, err = w.Write(buf)
		n += int64(wLen)
		if err != nil {
			return
		}
	}

	wLen, err = w.Write(b.Buf)
	n += int64(wLen)

	for _, buf := range b.bufs {
		putBuf(buf)
	}
	putBuf(b.toPool)

	b.bufs = nil
	b.Buf = nil
	b.toPool = nil
	return
}

// ReadFrom
func (b *Buffer) ReadFrom(r io.Reader) (int64, error) {

	var (
		c    = 4096
		buf  [4096]byte
		i, n int
	)

	for {
		m, e := r.Read(buf[i:c])
		i += m
		if i == c {
			b.AppendBytes(buf[:i])
			n += i
			i = 0
		}

		if e != nil {
			if i > 0 {
				b.AppendBytes(buf[:i])
				n += i
			}

			if e == io.EOF {
				return int64(n), nil
			}
			return int64(n), e
		}
	}
}

func (b *Buffer) Bytes() []byte {
	var ret = make([]byte, 0, b.Size())
	for _, buf := range b.bufs {
		ret = append(ret, buf...)
	}

	ret = append(ret, b.Buf...)
	return ret
}

func (b *Buffer) String() string {
	var ret = b.Bytes()
	return unsafe.String(unsafe.SliceData(ret), len(ret))
}

func (r *readCloser) WriteTo(w io.Writer) (n int64, err error) {
	bufs := net.Buffers(r.bufs)
	n, err = bufs.WriteTo(w)
	for _, buf := range r.bufs {
		putBuf(buf)
	}

	r.bufs = nil
	return n, err
}

func (r *readCloser) String() string {
	var n int
	for _, buf := range r.bufs {
		n += len(buf)
	}

	if n == 0 {
		return ""
	}

	var s = make([]byte, 0, n)
	for _, buf := range r.bufs {
		s = append(s, buf...)
	}

	return unsafe.String(unsafe.SliceData(s), len(s))
}

// ReadCloser creates an io.ReadCloser with all the contents of the buffer.
func (b *Buffer) RecyclableReader() RecyclableReader {

	ret := newRecyclableReadCloser(append(b.bufs, b.Buf))
	b.bufs = nil
	b.toPool = nil
	b.Buf = nil

	return ret
}

var closedErr = errors.New("reader is closed")
var (
	recyclableReadCloserPool = sync.Pool{New: func() any { return &recyclableReadCloser{} }}
	recyclablePool           = sync.Pool{New: func() any { return &recyclable{} }}
)

type RecyclableReader interface {
	io.ReadCloser
	WriteTo(w io.Writer) (n int64, err error)
	Clone() RecyclableReader
	Recycle()
	Len() int
	Bytes() []byte
	String() string
	Reset()
	BuildBytes(reuse []byte) []byte
}

type recyclable struct {
	data      [][]byte
	isRecycle atomic.Int64
}

func newRecyclable(data [][]byte) *recyclable {
	d := recyclablePool.Get().(*recyclable)
	d.data = data
	d.isRecycle = atomic.Int64{}
	d.isRecycle.Add(1)
	return d
}

func (r *recyclable) Recycle() {
	if r == nil {
		return
	}

	if r.isRecycle.Add(-1) == 0 {
		// Release all buffers.
		data := r.data
		r.data = nil

		for _, buf := range data {
			putBuf(buf)
		}
		recyclablePool.Put(r)
	}
}

type recyclableReadCloser struct {
	bufs      *recyclable
	offset    int
	index     int
	offsetLen int
	size      int
	isClose   atomic.Bool
	isRecycle atomic.Bool
}

func newRecyclableReadCloser(data [][]byte) *recyclableReadCloser {
	d := recyclableReadCloserPool.Get().(*recyclableReadCloser)
	*d = recyclableReadCloser{bufs: newRecyclable(data)}
	return d
}

func (r *recyclableReadCloser) Len() int {
	if r == nil {
		return 0
	}

	if r.size > 0 {
		return r.size
	} else {
		bufs := r.bufs
		if bufs == nil {
			return 0
		}

		var n int
		for _, b := range bufs.data {
			n += len(b)
		}

		r.size = n
		return n
	}
}

func (r *recyclableReadCloser) Recycle() {
	if r == nil {
		return
	}

	if r.isRecycle.Swap(true) {
		return
	}

	r.bufs.Recycle()
	r.bufs = nil
	recyclableReadCloserPool.Put(r)
}

func (r *recyclableReadCloser) Clone() RecyclableReader {
	if r == nil {
		return nil
	}

	bufs := r.bufs
	if bufs != nil {
		bufs.isRecycle.Add(1)
	}

	d := recyclableReadCloserPool.Get().(*recyclableReadCloser)
	*d = recyclableReadCloser{bufs: bufs}
	return d
}

func (r *recyclableReadCloser) Close() error {
	if r == nil {
		return nil
	}

	r.isClose.Swap(true)
	return nil
}

func (r *recyclableReadCloser) Read(p []byte) (n int, err error) {
	if r == nil {
		return 0, io.EOF
	}

	if r.offsetLen >= r.Len() {
		return 0, io.EOF
	}

	if r.isClose.Load() {
		return 0, closedErr
	}

	d := r.bufs
	if d == nil {
		return 0, io.EOF
	}

	var (
		bufs  = d.data
		count = len(bufs)
		size  = r.Len()
	)
	for r.index < count {
		if r.isClose.Load() {
			err = closedErr
			break
		}
		// Copy as much as we can.
		buf := bufs[r.index]
		x := copy(p[n:], buf[r.offset:])
		n += x // Increment how much we filled.

		if r.offset+x == len(buf) {
			// On to the next buffer.
			r.offset = 0
			r.index++
		} else {
			r.offset += x
		}
		r.offsetLen += x

		if n == len(p) {
			break
		}
	}

	// Did we empty the whole buffer?
	if r.offsetLen >= size {
		if len(p) == 0 {
			return 0, nil
		}

		err = io.EOF
	}
	return
}

func (r *recyclableReadCloser) WriteTo(w io.Writer) (n int64, err error) {
	if r == nil {
		return 0, nil
	}

	if r.offsetLen >= r.Len() {
		return 0, nil
	}

	if r.isClose.Load() {
		return 0, closedErr
	}

	bufs := r.bufs
	if bufs == nil {
		return 0, nil
	}

	var (
		data  = bufs.data
		count = len(data)
		x     int
	)
	for r.index < count {
		if r.isClose.Load() {
			err = closedErr
			return
		}

		buf := data[r.index]
		x, err = w.Write(buf[r.offset:])
		n += int64(x)

		if r.offset+x == len(buf) {
			r.offset = 0
			r.index++
		} else {
			r.offset += x
		}
		r.offsetLen += x

		if err != nil {
			return
		}
	}
	return
}

func (r *recyclableReadCloser) Bytes() []byte {
	if r == nil {
		return nil
	}

	bufs := r.bufs
	var n = r.Len()
	if n == 0 {
		return nil
	}

	var bf = make([]byte, 0, n)
	for _, buf := range bufs.data {
		bf = append(bf, buf...)
	}

	return bf
}

func (r *recyclableReadCloser) String() string {
	sb := r.Bytes()
	return unsafe.String(unsafe.SliceData(sb), len(sb))
}

func (r *recyclableReadCloser) Reset() {
	r.offset = 0
	r.index = 0
	r.offsetLen = 0
}

func (r *recyclableReadCloser) BuildBytes(reuse []byte) []byte {
	size := r.Len()
	bufs := r.bufs
	if size == 0 {
		return nil
	}

	for _, buf := range bufs.data {
		reuse = append(reuse, buf...)
	}

	return reuse
}
