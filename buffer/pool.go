// Package buffer implements a buffer for serialization, consisting of a chain of []byte-s to
// reduce copying and to allow reuse of individual chunks.
package buffer

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"unsafe"
)

// PoolConfig contains configuration for the allocation and reuse strategy.
type PoolConfig struct {
	StartSize  int // Minimum chunk size that is allocated.
	PooledSize int // Minimum chunk size that is reused, reusing chunks too small will result in overhead.
	MaxSize    int // Maximum chunk size that will be allocated.
}

var config = PoolConfig{
	StartSize:  128,
	PooledSize: 512,
	MaxSize:    32768,
}

// Reuse pool: chunk size -> pool.
var buffers = map[int]*sync.Pool{}

func initBuffers() {
	for l := config.PooledSize; l <= config.MaxSize; l *= 2 {
		buffers[l] = new(sync.Pool)
	}
}

func init() {
	initBuffers()
}

// Init sets up a non-default pooling and allocation strategy. Should be run before serialization is done.
func Init(cfg PoolConfig) {
	config = cfg
	initBuffers()
}

// putBuf puts a chunk to reuse pool if it can be reused.
func putBuf(buf []byte) {
	size := cap(buf)
	if size < config.PooledSize {
		return
	}
	if c := buffers[size]; c != nil {
		c.Put(buf[:0])
	}
}

// getBuf gets a chunk from reuse pool or creates a new one if reuse failed.
func getBuf(size int) []byte {
	if size >= config.PooledSize {
		if c := buffers[size]; c != nil {
			v := c.Get()
			if v != nil {
				return v.([]byte)
			}
		}
	}
	return make([]byte, 0, size)
}

// Buffer is a buffer optimized for serialization without extra copying.
type Buffer struct {

	// Buf is the current chunk that can be used for serialization.
	Buf []byte

	toPool []byte
	bufs   [][]byte
}

// EnsureSpace makes sure that the current chunk contains at least s free bytes,
// possibly creating a new chunk.
func (b *Buffer) EnsureSpace(s int) {
	if cap(b.Buf)-len(b.Buf) < s {
		b.ensureSpaceSlow(s)
	}
}

func (b *Buffer) ensureSpaceSlow(s int) {
	l := len(b.Buf)
	if l > 0 {
		if cap(b.toPool) != cap(b.Buf) {
			// Chunk was reallocated, toPool can be pooled.
			putBuf(b.toPool)
		}
		if cap(b.bufs) == 0 {
			b.bufs = make([][]byte, 0, 8)
		}
		b.bufs = append(b.bufs, b.Buf)
		l = cap(b.toPool) * 2
	} else {
		l = config.StartSize
	}

	if l > config.MaxSize {
		l = config.MaxSize
	}
	b.Buf = getBuf(l)
	b.toPool = b.Buf
}

// AppendByte appends a single byte to buffer.
func (b *Buffer) AppendByte(data byte) {
	b.EnsureSpace(1)
	b.Buf = append(b.Buf, data)
}

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

// AppendBytes appends a byte slice to buffer.
func (b *Buffer) AppendBytes(data []byte) {
	if len(data) <= cap(b.Buf)-len(b.Buf) {
		b.Buf = append(b.Buf, data...) // fast path
	} else {
		b.appendBytesSlow(data)
	}
}

func (b *Buffer) appendBytesSlow(data []byte) {
	for len(data) > 0 {
		b.EnsureSpace(1)

		sz := cap(b.Buf) - len(b.Buf)
		if sz > len(data) {
			sz = len(data)
		}

		b.Buf = append(b.Buf, data[:sz]...)
		data = data[sz:]
	}
}

// AppendString appends a string to buffer.
func (b *Buffer) AppendString(data string) {
	if len(data) <= cap(b.Buf)-len(b.Buf) {
		b.Buf = append(b.Buf, data...) // fast path
	} else {
		b.appendStringSlow(data)
	}
}

func (b *Buffer) appendStringSlow(data string) {
	for len(data) > 0 {
		b.EnsureSpace(1)

		sz := cap(b.Buf) - len(b.Buf)
		if sz > len(data) {
			sz = len(data)
		}

		b.Buf = append(b.Buf, data[:sz]...)
		data = data[sz:]
	}
}

// Size computes the size of a buffer by adding sizes of every chunk.
func (b *Buffer) Size() int {
	size := len(b.Buf)
	for _, buf := range b.bufs {
		size += len(buf)
	}
	return size
}

// DumpTo outputs the contents of a buffer to a writer and resets the buffer.
func (b *Buffer) DumpTo(w io.Writer) (written int, err error) {
	//bufs := net.Buffers(b.bufs)
	bufs := append(make([][]byte, 0, len(b.bufs)+1), b.bufs...)
	if len(b.Buf) > 0 {
		bufs = append(bufs, b.Buf)
	}

	n2 := net.Buffers(bufs)
	n, err := n2.WriteTo(w)

	for _, buf := range b.bufs {
		putBuf(buf)
	}
	putBuf(b.toPool)

	b.bufs = nil
	b.Buf = nil
	b.toPool = nil

	return int(n), err
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

// BuildBytes creates a single byte slice with all the contents of the buffer. Data is
// copied if it does not fit in a single chunk. You can optionally provide one byte
// slice as argument that it will try to reuse.
func (b *Buffer) BuildBytes(reuse ...[]byte) []byte {
	if len(b.bufs) == 0 {
		ret := b.Buf
		b.toPool = nil
		b.Buf = nil
		return ret
	}

	var ret []byte
	size := b.Size()

	// If we got a buffer as argument and it is big enough, reuse it.
	if len(reuse) == 1 && cap(reuse[0]) >= size {
		ret = reuse[0][:0]
	} else {
		ret = make([]byte, 0, size)
	}
	for _, buf := range b.bufs {
		ret = append(ret, buf...)
		putBuf(buf)
	}

	ret = append(ret, b.Buf...)
	putBuf(b.toPool)

	b.bufs = nil
	b.toPool = nil
	b.Buf = nil

	return ret
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

type readCloser struct {
	offset int
	bufs   [][]byte
}

func (r *readCloser) Read(p []byte) (n int, err error) {
	for _, buf := range r.bufs {
		// Copy as much as we can.
		x := copy(p[n:], buf[r.offset:])
		n += x // Increment how much we filled.

		// Did we empty the whole buffer?
		if r.offset+x == len(buf) {
			// On to the next buffer.
			r.offset = 0
			r.bufs = r.bufs[1:]

			// We can release this buffer.
			putBuf(buf)
		} else {
			r.offset += x
		}

		if n == len(p) {
			break
		}
	}
	// No buffers left or nothing read?
	if len(r.bufs) == 0 {
		err = io.EOF
	}
	return
}

func (r *readCloser) Close() error {
	// Release all remaining buffers.
	for _, buf := range r.bufs {
		putBuf(buf)
	}
	// In case Close gets called multiple times.
	r.bufs = nil

	return nil
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
func (b *Buffer) ReadCloser() io.ReadCloser {
	ret := &readCloser{0, append(b.bufs, b.Buf)}

	b.bufs = nil
	b.toPool = nil
	b.Buf = nil

	return ret
}

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
