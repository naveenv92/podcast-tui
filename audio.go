package main

import (
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep"
)

const outputSampleRate = beep.SampleRate(44100)

// swapPayload carries a pre-warmed reader and its pre-buffered audio to PCMStreamer.
type swapPayload struct {
	prebuf []byte
	reader io.ReadCloser
	gen    int
}

// seekDoneMsg is returned when a background seek finishes.
type seekDoneMsg struct {
	cmd *exec.Cmd
	gen int
}

// seekTimerMsg fires after the debounce delay; stale ones are dropped via seq.
type seekTimerMsg struct {
	target time.Duration
	seq    int
}

type tickMsg time.Time

type ffmpegErrMsg struct {
	generation int
	text       string
}

// PCMStreamer reads raw s16le stereo PCM from an ffmpeg pipe.
// It supports seamless reader swaps via pendSwap for skip-without-gap seeks.
type PCMStreamer struct {
	reader        io.ReadCloser
	prebuf        []byte
	prebufPos     int
	buf           [4]byte
	samplesPlayed int64 // accessed atomically
	pendSwap      chan swapPayload
}

func (s *PCMStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	// Non-blocking check: swap to pre-warmed reader if one is ready.
	select {
	case sw := <-s.pendSwap:
		s.reader.Close()
		s.reader = sw.reader
		s.prebuf = sw.prebuf
		s.prebufPos = 0
		atomic.StoreInt64(&s.samplesPlayed, 0)
	default:
	}

	for i := range samples {
		var frame [4]byte
		if s.prebufPos < len(s.prebuf) {
			copy(frame[:], s.prebuf[s.prebufPos:s.prebufPos+4])
			s.prebufPos += 4
		} else if _, err := io.ReadFull(s.reader, frame[:]); err != nil {
			return i, i > 0
		}
		samples[i][0] = float64(int16(frame[0])|int16(frame[1])<<8) / 32768.0
		samples[i][1] = float64(int16(frame[2])|int16(frame[3])<<8) / 32768.0
		atomic.AddInt64(&s.samplesPlayed, 1)
	}
	return len(samples), true
}

func (s *PCMStreamer) Err() error { return nil }

// buildAtempoFilter returns an ffmpeg -af value for pitch-preserving speed change.
// atempo only accepts [0.5, 2.0], so values above 2.0 are chained.
func buildAtempoFilter(speed float64) string {
	if speed <= 2.0 {
		return fmt.Sprintf("atempo=%.4f", speed)
	}
	return fmt.Sprintf("atempo=2.0,atempo=%.4f", speed/2.0)
}

// doSeek starts a new ffmpeg process at target, pre-reads ~93ms into a buffer,
// then queues an atomic reader swap into the live PCMStreamer so there is no
// audio gap. Old audio keeps playing until the swap fires.
func doSeek(pcm *PCMStreamer, oldCmd *exec.Cmd, audioURL string, target time.Duration, speed float64, gen int) tea.Cmd {
	return func() tea.Msg {
		args := []string{"-loglevel", "error"}
		if target > 0 {
			args = append(args, "-ss", fmt.Sprintf("%.3f", target.Seconds()))
		}
		args = append(args,
			"-i", audioURL,
			"-af", buildAtempoFilter(speed),
			"-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1",
		)
		cmd := exec.Command("ffmpeg", args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return seekDoneMsg{gen: gen}
		}
		if err := cmd.Start(); err != nil {
			return seekDoneMsg{gen: gen}
		}

		// Pre-read ~93ms (4096 frames × 4 bytes) so Stream never starves at swap time.
		prebuf := make([]byte, 4096*4)
		n, _ := io.ReadFull(stdout, prebuf)

		// Drain any stale pending swap before sending ours.
		// If the queued swap is from a newer generation, put it back and abort —
		// our payload is already superseded and its process will be killed when
		// seekDoneMsg is processed.
		select {
		case old := <-pcm.pendSwap:
			if old.gen >= gen {
				pcm.pendSwap <- old
				stdout.Close()
				cmd.Process.Kill()
				go cmd.Wait()
				return seekDoneMsg{cmd: nil, gen: gen}
			}
			old.reader.Close()
		default:
		}
		pcm.pendSwap <- swapPayload{prebuf: prebuf[:n], reader: stdout, gen: gen}

		// Kill the process that was playing before this seek started.
		if oldCmd != nil {
			oldCmd.Process.Kill()
			go oldCmd.Wait()
		}

		return seekDoneMsg{cmd: cmd, gen: gen}
	}
}

func tick() tea.Cmd {
	return tea.Every(time.Millisecond*500, func(t time.Time) tea.Msg { return tickMsg(t) })
}
