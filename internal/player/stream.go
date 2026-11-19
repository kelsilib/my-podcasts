package player

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/aac"
	_ "github.com/egoist/mygo/audio/flac"
	_ "github.com/egoist/mygo/audio/mp3"
	_ "github.com/egoist/mygo/audio/mp4"
	_ "github.com/egoist/mygo/audio/vorbis"
	"github.com/egoist/mygo/netproxy"
	gomp3 "github.com/hajimehoshi/go-mp3"
)

const streamUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var streamHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 netproxy.Proxy,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	},
}

// download streams an HTTP audio URL into a temporary file on disk (or memory
// fallback) so playback can start as soon as the first bytes arrive.
type download struct {
	mu     sync.Mutex
	cond   sync.Cond
	file   *os.File
	path   string
	data   []byte
	n      int64
	size   int64
	done   bool
	err    error
	cancel context.CancelFunc
	users  int
	closed bool
}

func startDownload(ctx context.Context, url string) (*download, error) {
	dlCtx, cancel := context.WithCancel(ctx)

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", streamUserAgent)

	res, err := streamHTTPClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		res.Body.Close()
		cancel()
		return nil, fmt.Errorf("audio stream HTTP %s", res.Status)
	}

	d := &download{
		size:   res.ContentLength,
		cancel: cancel,
		users:  2, // caller stream + background reader goroutine
	}
	d.cond.L = &d.mu

	if tmp, terr := os.CreateTemp("", "ap-stream-*.tmp"); terr == nil {
		d.file = tmp
		d.path = tmp.Name()
	} else if d.size > 0 && d.size < 32<<20 {
		d.data = make([]byte, 0, d.size)
	}

	go d.run(res.Body)
	return d, nil
}

func (d *download) run(body io.ReadCloser) {
	defer body.Close()
	defer d.release()

	finish := func(err error) {
		d.mu.Lock()
		d.done = true
		d.err = err
		d.cond.Broadcast()
		d.mu.Unlock()
	}

	buf := make([]byte, 64<<10)
	var written int64
	for {
		n, err := body.Read(buf)
		if n > 0 {
			d.writeAt(buf[:n], written)
			written += int64(n)
		}
		if err == io.EOF {
			finish(nil)
			return
		}
		if err != nil {
			finish(err)
			return
		}
	}
}

func (d *download) writeAt(b []byte, off int64) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	f := d.file
	d.mu.Unlock()

	if f != nil {
		if _, err := f.WriteAt(b, off); err != nil {
			d.fallbackToMemory()
		}
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if d.file == nil {
		d.data = append(d.data, b...)
	}
	d.n += int64(len(b))
	d.cond.Broadcast()
	d.mu.Unlock()
}

func (d *download) fallbackToMemory() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.file == nil {
		return
	}
	mem := make([]byte, d.n)
	got, _ := d.file.ReadAt(mem, 0)
	d.data = mem[:got]
	d.n = int64(got)
	d.file.Close()
	if d.path != "" {
		os.Remove(d.path)
	}
	d.file = nil
	d.path = ""
}

// Progress returns the fraction of the stream downloaded so far in [0, 1].
func (d *download) Progress() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done && d.err == nil {
		return 1
	}
	if d.size <= 0 {
		return 0
	}
	return min(1, max(0, float64(d.n)/float64(d.size)))
}

func (d *download) Close() error {
	if d.cancel != nil {
		d.cancel()
	}
	d.mu.Lock()
	if !d.done {
		d.done = true
		d.err = context.Canceled
	}
	d.cond.Broadcast()
	alreadyClosed := d.closed
	d.closed = true
	d.mu.Unlock()
	if !alreadyClosed {
		d.release()
	}
	return nil
}

func (d *download) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.users--
	if d.users > 0 {
		return
	}
	if d.file != nil {
		d.file.Close()
		d.file = nil
	}
	if d.path != "" {
		os.Remove(d.path)
		d.path = ""
	}
	d.data = nil
	d.n = 0
}

func (d *download) waitFor(n int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n < n && !d.done {
		d.cond.Wait()
	}
	return d.n
}

func (d *download) readAt(b []byte, off int64) (int, error) {
	n := min(int64(len(b)), d.n-off)
	if n <= 0 {
		return 0, nil
	}
	if d.file == nil {
		return copy(b, d.data[off:d.n]), nil
	}
	return d.file.ReadAt(b[:n], off)
}

func (d *download) peek(off int64, n int) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	b := make([]byte, n)
	got, _ := d.readAt(b, off)
	return b[:got]
}

// downloadReader is a sequential io.Reader (intentionally NOT implementing
// io.Seeker) so go-mp3 starts decoding immediately without reading to EOF.
type downloadReader struct {
	d   *download
	pos int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	d := r.d
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n <= r.pos && !d.done {
		d.cond.Wait()
	}
	if d.n <= r.pos {
		if d.err != nil {
			return 0, d.err
		}
		return 0, io.EOF
	}
	n, err := d.readAt(p, r.pos)
	if n == 0 {
		return 0, cmp.Or(err, io.ErrUnexpectedEOF)
	}
	r.pos += int64(n)
	return n, nil
}

// downloadReadSeeker implements io.ReadSeekCloser for container formats (M4A,
// AAC, FLAC, Ogg Vorbis, WAV) decoded through audio.NewDecoder.
type downloadReadSeeker struct {
	d   *download
	pos int64
}

func (r *downloadReadSeeker) Read(p []byte) (int, error) {
	d := r.d
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n <= r.pos && !d.done {
		d.cond.Wait()
	}
	if d.n <= r.pos {
		if d.err != nil {
			return 0, d.err
		}
		return 0, io.EOF
	}
	n, err := d.readAt(p, r.pos)
	if n == 0 {
		return 0, cmp.Or(err, io.ErrUnexpectedEOF)
	}
	r.pos += int64(n)
	return n, nil
}

func (r *downloadReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		base = r.pos
	case io.SeekEnd:
		r.d.mu.Lock()
		sz := r.d.size
		done := r.d.done
		n := r.d.n
		r.d.mu.Unlock()
		switch {
		case sz > 0:
			base = sz
		case done:
			base = n
		default:
			base = r.d.waitFor(math.MaxInt64)
		}
	default:
		return 0, errors.New("player: invalid whence")
	}
	target := base + offset
	if target < 0 {
		return 0, errors.New("player: negative seek position")
	}
	r.pos = target
	return target, nil
}

func (r *downloadReadSeeker) Close() error {
	return r.d.Close()
}

type closingDecoder struct {
	audio.Decoder
	io.Closer
}

func openHTTPStream(ctx context.Context, assetURL string, durationMS int64) (audio.Decoder, *download, error) {
	dl, err := startDownload(ctx, assetURL)
	if err != nil {
		return nil, nil, err
	}

	dl.waitFor(12)
	if head := dl.peek(0, 12); len(head) == 12 {
		if string(head[4:8]) == "ftyp" || ((string(head[:4]) == "RIFF" || string(head[:4]) == "RF64") && string(head[8:12]) == "WAVE") {
			dec, _, derr := audio.NewDecoder(&downloadReadSeeker{d: dl})
			if derr != nil {
				dl.Close()
				return nil, nil, derr
			}
			return closingDecoder{Decoder: dec, Closer: dl}, dl, nil
		}
	}

	off := id3Size(dl)
	if dl.waitFor(off+7) >= off+4 {
		head := dl.peek(off, 7)
		if string(head[:4]) == "fLaC" || string(head[:4]) == "OggS" || isADTS(head) {
			dec, _, derr := audio.NewDecoder(&downloadReadSeeker{d: dl})
			if derr != nil {
				dl.Close()
				return nil, nil, derr
			}
			return closingDecoder{Decoder: dec, Closer: dl}, dl, nil
		}
	}

	dec, err := openMP3Stream(dl, durationMS)
	if err != nil {
		dl.Close()
		return nil, nil, err
	}
	return dec, dl, nil
}

func isADTS(head []byte) bool {
	if len(head) < 7 || head[0] != 0xff || head[1]&0xf6 != 0xf0 {
		return false
	}
	rateIdx := (head[2] >> 2) & 0x0f
	frameLen := int(head[3]&0x03)<<11 | int(head[4])<<3 | int(head[5]>>5)
	return rateIdx < 13 && frameLen >= 7
}

func id3Size(dl *download) int64 {
	dl.waitFor(10)
	b := dl.peek(0, 10)
	if len(b) < 10 || string(b[:3]) != "ID3" {
		return 0
	}
	size := int64(b[6]&0x7f)<<21 | int64(b[7]&0x7f)<<14 | int64(b[8]&0x7f)<<7 | int64(b[9]&0x7f)
	return 10 + size
}

// mp3Stream progressively decodes an MP3 download and supports byte-estimated seeking.
type mp3Stream struct {
	dl     *download
	dec    *gomp3.Decoder
	rate   int
	frames int64
	start  int64
	skip   int
	buf    []byte
}

func openMP3Stream(dl *download, durationMS int64) (*mp3Stream, error) {
	s := &mp3Stream{dl: dl, start: id3Size(dl)}
	dec, err := gomp3.NewDecoder(&downloadReader{d: dl})
	if err != nil {
		return nil, err
	}
	s.dec = dec
	s.rate = dec.SampleRate()
	if s.rate <= 0 {
		s.rate = 44100
	}
	if durationMS > 0 {
		s.frames = durationMS * int64(s.rate) / 1000
	} else {
		s.frames = s.estimateFrames()
	}
	return s, nil
}

func (s *mp3Stream) SampleRate() int { return s.rate }
func (s *mp3Stream) Channels() int   { return 2 }
func (s *mp3Stream) Len() int64      { return s.frames }
func (s *mp3Stream) Close() error    { return s.dl.Close() }

func (s *mp3Stream) Read(dst []float32) (int, error) {
	n := len(dst) / 2 * 4
	if n == 0 {
		return 0, nil
	}
	if cap(s.buf) < n {
		s.buf = make([]byte, n)
	}
	b := s.buf[:n]

	for s.skip > 0 {
		got, err := io.ReadFull(s.dec, b[:min(n, s.skip)])
		s.skip -= got
		if err != nil {
			s.skip = 0
			break
		}
	}

	got, err := io.ReadFull(s.dec, b)
	got -= got % 4
	samples := got / 2
	for i := range samples {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(b[2*i:]))) / (1 << 15)
	}
	if samples > 0 {
		return samples, nil
	}
	if err == io.ErrUnexpectedEOF || err == nil {
		err = io.EOF
	}
	return 0, err
}

const mp3PrerollFrames = 4 * 1152

func (s *mp3Stream) SetPosition(frame int64) error {
	if frame <= 0 {
		dec, err := gomp3.NewDecoder(&downloadReader{d: s.dl, pos: 0})
		if err != nil {
			return err
		}
		s.dec = dec
		s.skip = 0
		return nil
	}

	var off int64
	from := max(int64(0), frame-mp3PrerollFrames)
	if from > 0 && s.frames > 0 {
		size := s.estimateTotalSize()
		if size > s.start {
			off = s.start + int64(float64(size-s.start)*float64(from)/float64(s.frames))
			off = findMP3Frame(s.dl, off)
		}
	}
	dec, err := gomp3.NewDecoder(&downloadReader{d: s.dl, pos: off})
	if err != nil {
		return err
	}
	s.dec = dec
	s.skip = int(frame-from) * 4
	return nil
}

func (s *mp3Stream) estimateTotalSize() int64 {
	if s.dl.size > 0 {
		return s.dl.size
	}
	s.dl.mu.Lock()
	done, n := s.dl.done, s.dl.n
	s.dl.mu.Unlock()
	if done {
		return n
	}
	s.dl.waitFor(s.start + 4)
	if kbps := mp3BitRate(s.dl.peek(s.start, 4)); kbps > 0 && s.frames > 0 {
		return s.start + s.frames*int64(kbps)*1000/8/int64(s.rate)
	}
	return s.dl.waitFor(math.MaxInt64)
}

func (s *mp3Stream) estimateFrames() int64 {
	if s.dl.size <= 0 {
		return -1
	}
	s.dl.waitFor(s.start + 4)
	kbps := mp3BitRate(s.dl.peek(s.start, 4))
	if kbps <= 0 {
		return -1
	}
	audioBytes := max(int64(0), s.dl.size-s.start)
	return audioBytes * 8 * int64(s.rate) / (int64(kbps) * 1000)
}

var (
	mp3BitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3Rates      = [4]int{44100, 48000, 32000, 0}
)

func mp3BitRate(b []byte) int {
	if mp3FrameSize(b) == 0 {
		return 0
	}
	if (b[1]>>3)&3 == 3 {
		return mp3BitratesV1[b[2]>>4]
	}
	return mp3BitratesV2[b[2]>>4]
}

func mp3FrameSize(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return 0
	}
	version := (b[1] >> 3) & 3
	layer := (b[1] >> 1) & 3
	if version == 1 || layer != 1 {
		return 0
	}
	bi, ri := b[2]>>4, (b[2]>>2)&3
	if bi == 0 || bi == 15 || ri == 3 {
		return 0
	}
	pad := int(b[2]>>1) & 1
	rate := mp3Rates[ri]
	switch version {
	case 3:
		return 144*mp3BitratesV1[bi]*1000/rate + pad
	case 2:
		return 72*mp3BitratesV2[bi]*1000/(rate/2) + pad
	default:
		return 72*mp3BitratesV2[bi]*1000/(rate/4) + pad
	}
}

// findMP3Frame scans forward from off for two consecutive valid MP3 frames.
func findMP3Frame(dl *download, off int64) int64 {
	const scanLimit = 64 << 10
	dl.waitFor(off + scanLimit)
	buf := dl.peek(off, scanLimit)
	for i := 0; i+4 <= len(buf); i++ {
		sz := mp3FrameSize(buf[i : i+4])
		if sz == 0 {
			continue
		}
		if i+sz+4 <= len(buf) && mp3FrameSize(buf[i+sz:i+sz+4]) > 0 {
			return off + int64(i)
		}
	}
	return off
}
