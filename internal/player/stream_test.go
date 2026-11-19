package player

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"testing"
	"time"
)

type streamTestTransport func(*http.Request) (*http.Response, error)

func (f streamTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type stalledStreamBody struct {
	ctx     context.Context
	started chan struct{}
	release chan struct{}
}

func (b *stalledStreamBody) Read([]byte) (int, error) {
	close(b.started)
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.release:
		return 0, io.EOF
	}
}

func (*stalledStreamBody) Close() error { return nil }

func TestCancelStreamAfterResponseHeaders(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	client := streamHTTPClient
	streamHTTPClient = &http.Client{Transport: streamTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, ContentLength: -1,
			Body: &stalledStreamBody{ctx: req.Context(), started: started, release: release},
		}, nil
	})}
	t.Cleanup(func() { streamHTTPClient = client })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, dl, err := openHTTPStream(ctx, "https://example.test/stalled.mp3", 0)
		if dl != nil {
			dl.Close()
		}
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not reach the response body")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("openHTTPStream after cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("openHTTPStream is still waiting for the response body after cancellation")
	}
}

func TestOpenHEAACStream(t *testing.T) {
	const testURL = "https://tk.wavpub.com/WPTK_Cq7Vhv8ixsdEqR1R.m4a"

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	dec, dl, err := openHTTPStream(ctx, testURL, 0)
	if err != nil {
		t.Fatalf("openHTTPStream failed: %v", err)
	}
	if c, ok := dec.(io.Closer); ok {
		defer c.Close()
	} else if dl != nil {
		defer dl.Close()
	}

	if got := dec.SampleRate(); got != 44100 {
		t.Fatalf("SampleRate() = %d, want 44100", got)
	}
	if got := dec.Channels(); got != 2 {
		t.Fatalf("Channels() = %d, want 2", got)
	}
	if got := dec.Len(); got <= 0 {
		t.Fatalf("Len() = %d, want > 0", got)
	}

	targetSamples := dec.SampleRate() * dec.Channels() * 2
	buf := make([]float32, 8192)
	var total int
	var peak float64
	for total < targetSamples {
		n, err := dec.Read(buf)
		for _, s := range buf[:n] {
			if a := math.Abs(float64(s)); a > peak {
				peak = a
			}
		}
		total += n
		if err != nil {
			t.Fatalf("dec.Read failed after %d samples: %v", total, err)
		}
	}
	if total <= 44100 {
		t.Fatalf("decoded only %d samples, want > 44100", total)
	}
	if peak <= 0.01 {
		t.Fatalf("decoded peak amplitude = %f, want > 0.01", peak)
	}

	seekFrame := int64(dec.SampleRate()) * 60
	if err := dec.SetPosition(seekFrame); err != nil {
		t.Fatalf("SetPosition(%d) failed: %v", seekFrame, err)
	}

	var seekTotal int
	var seekPeak float64
	for seekTotal < dec.SampleRate()*dec.Channels() {
		n, err := dec.Read(buf)
		for _, s := range buf[:n] {
			if a := math.Abs(float64(s)); a > seekPeak {
				seekPeak = a
			}
		}
		seekTotal += n
		if err != nil {
			t.Fatalf("dec.Read after seek failed after %d samples: %v", seekTotal, err)
		}
	}
	if seekPeak <= 0.01 {
		t.Fatalf("post-seek peak amplitude = %f, want > 0.01", seekPeak)
	}
}

func TestDownloadWriteAndAppendSamplesSafety(t *testing.T) {
	d := &download{users: 2}
	d.cond.L = &d.mu
	d.Close()
	// Writing after close must remain safe and not allocate memory buffer.
	d.writeAt([]byte{1, 2, 3}, 0)
	if len(d.data) != 0 {
		t.Fatalf("expected closed download not to allocate memory buffer, got %d bytes", len(d.data))
	}

	if got := appendDecodedSamples(nil, []byte{1, 2}, 2, true, 32); len(got) != 0 {
		t.Fatalf("expected short float buffer to return empty slice, got %v", got)
	}
	// 16-bit mono -> stereo duplication
	if got := appendDecodedSamples(nil, []byte{0x00, 0x40}, 1, false, 16); len(got) != 2 || got[0] != got[1] || got[0] <= 0 {
		t.Fatalf("unexpected 16-bit mono expansion: %v", got)
	}
}
