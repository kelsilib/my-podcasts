//go:build windows

package player

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/audio/aac"
	"github.com/egoist/mygo/audio/mp4"
)

func init() {
	mp4.RegisterCodec("mp4a", openMP4ACodec)
}

type mfGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	ole32DLL                 = syscall.NewLazyDLL("ole32.dll")
	mfplatDLL                = syscall.NewLazyDLL("mfplat.dll")
	procCoInitializeEx       = ole32DLL.NewProc("CoInitializeEx")
	procCoCreateInstance     = ole32DLL.NewProc("CoCreateInstance")
	procMFStartup            = mfplatDLL.NewProc("MFStartup")
	procMFCreateMediaType    = mfplatDLL.NewProc("MFCreateMediaType")
	procMFCreateSample       = mfplatDLL.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer = mfplatDLL.NewProc("MFCreateMemoryBuffer")

	mfInitOnce sync.Once
	mfInitErr  error

	clsidCMSAACDecMFT = mfGUID{0x32d186a7, 0x218f, 0x4c75, [8]byte{0x88, 0x76, 0xdd, 0x77, 0x27, 0x3a, 0x89, 0x99}}
	iidIMFTransform   = mfGUID{0xbf94c121, 0x5b05, 0x4e6f, [8]byte{0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d}}

	mfMTMajorType               = mfGUID{0x48eba18e, 0xf8c9, 0x4687, [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}}
	mfMTSubType                 = mfGUID{0xf7e34c9a, 0x42e8, 0x4714, [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}}
	mfMTAudioPayloadType        = mfGUID{0xbfbabe79, 0x7434, 0x4d1c, [8]byte{0x94, 0xf0, 0x72, 0xa3, 0xb9, 0xe1, 0x71, 0x88}}
	mfMTAudioSamplesPerSecond   = mfGUID{0x5faeeae7, 0x0290, 0x4c31, [8]byte{0x9e, 0x8a, 0xc5, 0x34, 0xf6, 0x8d, 0x9d, 0xba}}
	mfMTAudioNumChannels        = mfGUID{0x37e48bf5, 0x645e, 0x4c5b, [8]byte{0x89, 0xde, 0xad, 0xa9, 0xe2, 0x9b, 0x69, 0x6a}}
	mfMTUserData                = mfGUID{0xb6bc765f, 0x4c3b, 0x40a4, [8]byte{0xbd, 0x51, 0x25, 0x35, 0xb6, 0x6f, 0xe0, 0x9d}}
	mfMTAudioBitsPerSample      = mfGUID{0xf2deb57f, 0x40fa, 0x4764, [8]byte{0xaa, 0x33, 0xed, 0x4f, 0x2d, 0x1f, 0xf6, 0x69}}
	mfMediaTypeAudio            = mfGUID{0x73647561, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfAudioFormatAAC            = mfGUID{0x00001610, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfAudioFormatFloat          = mfGUID{0x00000003, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfAudioFormatPCM            = mfGUID{0x00000001, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
)

const (
	mfVersion                    = 0x00020070
	mfStartupNoSocket            = 0x1
	mftMsgCommandFlush           = 0x00000000
	mftMsgNotifyBeginStreaming   = 0x10000000
	mftMsgNotifyStartOfStream    = 0x10000003
	mfENotAccepting       uint32 = 0xC00D36B5
	mfETransformChange    uint32 = 0xC00D6D61
	mfETransformNeedInput uint32 = 0xC00D6D72
)

var ascSampleRates = [...]int{
	96000, 88200, 64000, 48000, 44100, 32000,
	24000, 22050, 16000, 12000, 11025, 8000, 7350,
}

var ascChannels = [...]int{0, 1, 2, 3, 4, 5, 6, 8}

func parseCoreASC(asc []byte) (aot, sampleRate, channels int) {
	if len(asc) < 2 {
		return 0, 44100, 2
	}
	var bitPos int
	readBits := func(n int) int {
		v := 0
		for range n {
			byteIdx := bitPos >> 3
			bitIdx := 7 - (bitPos & 7)
			bitPos++
			v <<= 1
			if byteIdx < len(asc) && (asc[byteIdx]&(1<<bitIdx)) != 0 {
				v |= 1
			}
		}
		return v
	}

	aot = readBits(5)
	if aot == 31 {
		aot = 32 + readBits(6)
	}
	sfIdx := readBits(4)
	switch {
	case sfIdx == 15:
		sampleRate = readBits(24)
	case sfIdx >= 0 && sfIdx < len(ascSampleRates):
		sampleRate = ascSampleRates[sfIdx]
	default:
		sampleRate = 44100
	}
	chCfg := readBits(4)
	if chCfg > 0 && chCfg < len(ascChannels) {
		channels = ascChannels[chCfg]
	} else {
		channels = 2
	}
	return aot, sampleRate, channels
}

func openMP4ACodec(asc []byte) (mp4.Codec, error) {
	_, coreRate, _ := parseCoreASC(asc)
	// HE-AAC with implicit SBR often signals only 2 bytes in AudioSpecificConfig at <= 24 kHz.
	// Explicit HE-AAC / HE-AAC v2 fails aac.NewCodec immediately. In both cases use Windows MF MFT.
	if len(asc) == 2 && coreRate > 24000 {
		if c, err := aac.NewCodec(asc); err == nil {
			return &fallbackAACCodec{inner: c, asc: append([]byte(nil), asc...)}, nil
		}
	}
	if mfCodec, err := newMFAACCodec(asc); err == nil {
		return mfCodec, nil
	}
	return aac.NewCodec(asc)
}

// fallbackAACCodec starts with go-aac for standard AAC-LC and falls back to
// Windows Media Foundation if a packet triggers an SBR/PS bitstream error.
type fallbackAACCodec struct {
	inner     mp4.Codec
	asc       []byte
	decodedOK bool
	switched  bool
}

func (f *fallbackAACCodec) SampleRate() int   { return f.inner.SampleRate() }
func (f *fallbackAACCodec) Reset()            { f.inner.Reset() }
func (f *fallbackAACCodec) Preroll() int      { return f.inner.Preroll() }
func (f *fallbackAACCodec) PacketFrames() int { return f.inner.PacketFrames() }

func (f *fallbackAACCodec) Decode(dst []float32, packet []byte) ([]float32, error) {
	out, err := f.inner.Decode(dst, packet)
	if err == nil {
		f.decodedOK = true
		return out, nil
	}
	if !f.switched {
		if mfCodec, mfErr := newMFAACCodec(f.asc); mfErr == nil {
			f.inner = mfCodec
			f.switched = true
			return f.inner.Decode(dst, packet)
		}
	}
	return out, err
}

func ensureMF() error {
	mfInitOnce.Do(func() {
		if err := ole32DLL.Load(); err != nil {
			mfInitErr = err
			return
		}
		if err := mfplatDLL.Load(); err != nil {
			mfInitErr = err
			return
		}
		_, _, _ = procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
		hr, _, _ := procMFStartup.Call(mfVersion, mfStartupNoSocket)
		if int32(hr) < 0 {
			mfInitErr = fmt.Errorf("MFStartup failed: 0x%08x", uint32(hr))
		}
	})
	return mfInitErr
}

func nativePtr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

//go:uintptrescapes
func comCall(obj uintptr, idx int, args ...uintptr) uint32 {
	vtbl := *(*uintptr)(nativePtr(obj))
	fn := *(*uintptr)(unsafe.Add(nativePtr(vtbl), idx*int(unsafe.Sizeof(uintptr(0)))))
	var a [8]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return uint32(r)
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

type mftOutputDataBuffer struct {
	dwStreamID uint32
	_pad0      uint32
	pSample    uintptr
	dwStatus   uint32
	_pad1      uint32
	pEvents    uintptr
}

type mftOutputStreamInfo struct {
	dwFlags     uint32
	cbSize      uint32
	cbAlignment uint32
}

type mfAACCodec struct {
	mu      sync.Mutex
	mft     uintptr
	rate    int
	ch      int
	bits    int
	isFloat bool
	outSize uint32
}

func newMFAACCodec(asc []byte) (*mfAACCodec, error) {
	if err := ensureMF(); err != nil {
		return nil, err
	}

	// On worker goroutines that may hop OS threads, ensure COM MTA or ignore RPC_E_CHANGED_MODE.
	_, _, _ = procCoInitializeEx.Call(0, 0)

	var mft uintptr
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCMSAACDecMFT)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIMFTransform)),
		uintptr(unsafe.Pointer(&mft)),
	)
	if int32(r) < 0 || mft == 0 {
		return nil, fmt.Errorf("CoCreateInstance(CMSAACDecMFT) failed: 0x%08x", uint32(r))
	}

	var inType uintptr
	r, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&inType)))
	if int32(r) < 0 || inType == 0 {
		comRelease(mft)
		return nil, fmt.Errorf("MFCreateMediaType failed: 0x%08x", uint32(r))
	}
	defer comRelease(inType)

	_, coreRate, coreCh := parseCoreASC(asc)
	if coreRate <= 0 {
		coreRate = 44100
	}
	if coreCh <= 0 {
		coreCh = 2
	}

	// IMFAttributes: 21=SetUINT32, 24=SetGUID, 26=SetBlob
	comCall(inType, 24, uintptr(unsafe.Pointer(&mfMTMajorType)), uintptr(unsafe.Pointer(&mfMediaTypeAudio)))
	comCall(inType, 24, uintptr(unsafe.Pointer(&mfMTSubType)), uintptr(unsafe.Pointer(&mfAudioFormatAAC)))
	comCall(inType, 21, uintptr(unsafe.Pointer(&mfMTAudioPayloadType)), 0)
	comCall(inType, 21, uintptr(unsafe.Pointer(&mfMTAudioSamplesPerSecond)), uintptr(coreRate))
	comCall(inType, 21, uintptr(unsafe.Pointer(&mfMTAudioNumChannels)), uintptr(coreCh))

	// HEAACWAVEINFO (12 bytes) + AudioSpecificConfig
	ud := make([]byte, 12+len(asc))
	binary.LittleEndian.PutUint16(ud[2:4], 0xfe) // wAudioProfileLevelIndication unspecified
	copy(ud[12:], asc)
	comCall(inType, 26, uintptr(unsafe.Pointer(&mfMTUserData)), uintptr(unsafe.Pointer(&ud[0])), uintptr(len(ud)))

	if hr := comCall(mft, 15, 0, inType, 0); int32(hr) < 0 { // IMFTransform::SetInputType
		comRelease(mft)
		return nil, fmt.Errorf("CMSAACDecMFT SetInputType failed: 0x%08x", hr)
	}

	c := &mfAACCodec{mft: mft}
	if err := c.negotiateOutputType(); err != nil {
		comRelease(mft)
		return nil, err
	}

	comCall(mft, 23, mftMsgNotifyBeginStreaming, 0)
	comCall(mft, 23, mftMsgNotifyStartOfStream, 0)

	runtime.SetFinalizer(c, (*mfAACCodec).close)
	return c, nil
}

func (c *mfAACCodec) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mft != 0 {
		comRelease(c.mft)
		c.mft = 0
	}
}

func (c *mfAACCodec) negotiateOutputType() error {
	// Prefer 32-bit float output, fall back to integer PCM.
	for _, wantFloat := range []bool{true, false} {
		for i := range 16 {
			var avail uintptr
			hr := comCall(c.mft, 14, 0, uintptr(i), uintptr(unsafe.Pointer(&avail))) // GetOutputAvailableType
			if int32(hr) < 0 || avail == 0 {
				break
			}
			var sub mfGUID
			var rate, ch, bits uint32
			// IMFAttributes: 7=GetUINT32, 10=GetGUID
			comCall(avail, 10, uintptr(unsafe.Pointer(&mfMTSubType)), uintptr(unsafe.Pointer(&sub)))
			comCall(avail, 7, uintptr(unsafe.Pointer(&mfMTAudioSamplesPerSecond)), uintptr(unsafe.Pointer(&rate)))
			comCall(avail, 7, uintptr(unsafe.Pointer(&mfMTAudioNumChannels)), uintptr(unsafe.Pointer(&ch)))
			comCall(avail, 7, uintptr(unsafe.Pointer(&mfMTAudioBitsPerSample)), uintptr(unsafe.Pointer(&bits)))

			match := (!wantFloat && sub == mfAudioFormatPCM) || (wantFloat && sub == mfAudioFormatFloat)
			if match && ch >= 1 {
				if hr := comCall(c.mft, 16, 0, avail, 0); int32(hr) == 0 { // SetOutputType
					c.rate = int(rate)
					c.ch = int(ch)
					c.isFloat = wantFloat
					if bits == 0 {
						if wantFloat {
							bits = 32
						} else {
							bits = 16
						}
					}
					c.bits = int(bits)

					var osi mftOutputStreamInfo
					comCall(c.mft, 7, 0, uintptr(unsafe.Pointer(&osi))) // GetOutputStreamInfo
					c.outSize = max(uint32(65536), osi.cbSize)
					comRelease(avail)
					return nil
				}
			}
			comRelease(avail)
		}
	}
	return errors.New("CMSAACDecMFT: no supported output media type")
}

func (c *mfAACCodec) SampleRate() int   { return c.rate }
func (c *mfAACCodec) Preroll() int      { return 1 }
func (c *mfAACCodec) PacketFrames() int { return 0 }

func (c *mfAACCodec) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mft == 0 {
		return
	}
	comCall(c.mft, 23, mftMsgCommandFlush, 0)
	comCall(c.mft, 23, mftMsgNotifyStartOfStream, 0)
}

func (c *mfAACCodec) Decode(dst []float32, packet []byte) ([]float32, error) {
	if len(packet) == 0 {
		return dst, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mft == 0 {
		return dst, errors.New("mft closed")
	}

	var inBuf, inSample uintptr
	r, _, _ := procMFCreateMemoryBuffer.Call(uintptr(len(packet)), uintptr(unsafe.Pointer(&inBuf)))
	if int32(r) < 0 || inBuf == 0 {
		return dst, fmt.Errorf("MFCreateMemoryBuffer failed: 0x%08x", uint32(r))
	}
	defer comRelease(inBuf)

	var ptr unsafe.Pointer
	if hr := comCall(inBuf, 3, uintptr(unsafe.Pointer(&ptr)), 0, 0); int32(hr) < 0 {
		return dst, fmt.Errorf("IMFMediaBuffer::Lock failed: 0x%08x", hr)
	}
	copy(unsafe.Slice((*byte)(ptr), len(packet)), packet)
	comCall(inBuf, 4) // Unlock
	comCall(inBuf, 6, uintptr(len(packet)))

	r, _, _ = procMFCreateSample.Call(uintptr(unsafe.Pointer(&inSample)))
	if int32(r) < 0 || inSample == 0 {
		return dst, fmt.Errorf("MFCreateSample failed: 0x%08x", uint32(r))
	}
	defer comRelease(inSample)

	if hr := comCall(inSample, 42, inBuf); int32(hr) < 0 { // AddBuffer
		return dst, fmt.Errorf("IMFSample::AddBuffer failed: 0x%08x", hr)
	}

	hr := comCall(c.mft, 24, 0, inSample, 0) // ProcessInput
	if hr == mfENotAccepting {
		var derr error
		dst, derr = c.drainOutputLocked(dst)
		if derr != nil {
			return dst, derr
		}
		hr = comCall(c.mft, 24, 0, inSample, 0)
	}
	if int32(hr) < 0 {
		return dst, fmt.Errorf("CMSAACDecMFT ProcessInput failed: 0x%08x", hr)
	}

	return c.drainOutputLocked(dst)
}

func (c *mfAACCodec) drainOutputLocked(dst []float32) ([]float32, error) {
	for {
		var outBuf, outSample uintptr
		r, _, _ := procMFCreateMemoryBuffer.Call(uintptr(c.outSize), uintptr(unsafe.Pointer(&outBuf)))
		if int32(r) < 0 || outBuf == 0 {
			return dst, fmt.Errorf("MFCreateMemoryBuffer(out) failed: 0x%08x", uint32(r))
		}
		r, _, _ = procMFCreateSample.Call(uintptr(unsafe.Pointer(&outSample)))
		if int32(r) < 0 || outSample == 0 {
			comRelease(outBuf)
			return dst, fmt.Errorf("MFCreateSample(out) failed: 0x%08x", uint32(r))
		}
		comCall(outSample, 42, outBuf) // AddBuffer

		var db mftOutputDataBuffer
		db.pSample = outSample
		var status uint32
		hr := comCall(c.mft, 25, 0, 1, uintptr(unsafe.Pointer(&db)), uintptr(unsafe.Pointer(&status))) // ProcessOutput
		if db.pEvents != 0 {
			comRelease(db.pEvents)
		}

		if hr == mfETransformChange {
			comRelease(outSample)
			comRelease(outBuf)
			if err := c.negotiateOutputType(); err != nil {
				return dst, err
			}
			continue
		}
		if hr == mfETransformNeedInput || int32(hr) < 0 {
			comRelease(outSample)
			comRelease(outBuf)
			break
		}

		var outPtr unsafe.Pointer
		var curLen uint32
		if lockHR := comCall(outBuf, 3, uintptr(unsafe.Pointer(&outPtr)), 0, uintptr(unsafe.Pointer(&curLen))); int32(lockHR) >= 0 && curLen > 0 {
			dst = appendDecodedSamples(dst, unsafe.Slice((*byte)(outPtr), int(curLen)), c.ch, c.isFloat, c.bits)
			comCall(outBuf, 4) // Unlock
		}
		comRelease(outSample)
		comRelease(outBuf)
	}
	return dst, nil
}

func appendDecodedSamples(dst []float32, raw []byte, ch int, isFloat bool, bits int) []float32 {
	if ch <= 0 || len(raw) == 0 {
		return dst
	}
	if isFloat {
		n := len(raw) / 4
		if n == 0 {
			return dst
		}
		floats := unsafe.Slice((*float32)(unsafe.Pointer(&raw[0])), n)
		if ch == 1 {
			for _, s := range floats {
				dst = append(dst, s, s)
			}
		} else {
			for i := 0; i+ch <= len(floats); i += ch {
				dst = append(dst, floats[i], floats[i+1])
			}
		}
		return dst
	}

	switch bits {
	case 16:
		n := len(raw) / 2
		if ch == 1 {
			for i := range n {
				s := float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / (1 << 15)
				dst = append(dst, s, s)
			}
		} else {
			for i := 0; i+ch <= n; i += ch {
				l := float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / (1 << 15)
				r := float32(int16(binary.LittleEndian.Uint16(raw[(i+1)*2:]))) / (1 << 15)
				dst = append(dst, l, r)
			}
		}
	case 32:
		n := len(raw) / 4
		if ch == 1 {
			for i := range n {
				s := float32(int32(binary.LittleEndian.Uint32(raw[i*4:]))) / (1 << 31)
				dst = append(dst, s, s)
			}
		} else {
			for i := 0; i+ch <= n; i += ch {
				l := float32(int32(binary.LittleEndian.Uint32(raw[i*4:]))) / (1 << 31)
				r := float32(int32(binary.LittleEndian.Uint32(raw[(i+1)*4:]))) / (1 << 31)
				dst = append(dst, l, r)
			}
		}
	}
	return dst
}
