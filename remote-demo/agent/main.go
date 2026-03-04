package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/kbinani/screenshot"
	"github.com/xconnio/xconn-go/xconn"
)

const (
	procStart  = "io.xconn.desktop.start"
	procStop   = "io.xconn.desktop.stop"
	procMouse  = "io.xconn.desktop.mouse"
	procKey    = "io.xconn.desktop.key"
	topicFrame = "io.xconn.desktop.frame"
)

type Agent struct {
	client      *xconn.Client
	mu          sync.Mutex
	streaming   bool
	cancel      context.CancelFunc
	frameWidth  int
	jpegQuality int
	fps         int
}

func main() {
	routerURL := envOrDefault("WAMP_URL", "ws://localhost:8080/ws")
	realm := envOrDefault("WAMP_REALM", "default")

	client, err := xconn.Connect(routerURL, realm)
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer client.Close()

	agent := &Agent{
		client:      client,
		frameWidth:  envIntOrDefault("FRAME_WIDTH", 1280),
		jpegQuality: envIntOrDefault("JPEG_QUALITY", 60),
		fps:         envIntOrDefault("FPS", 10),
	}

	if err := agent.registerRPCs(); err != nil {
		log.Fatalf("register failed: %v", err)
	}

	log.Println("desktop agent connected and ready")
	select {}
}

func (a *Agent) registerRPCs() error {
	if err := a.client.Register(procStart, a.handleStart); err != nil {
		return err
	}
	if err := a.client.Register(procStop, a.handleStop); err != nil {
		return err
	}
	if err := a.client.Register(procMouse, a.handleMouse); err != nil {
		return err
	}
	if err := a.client.Register(procKey, a.handleKey); err != nil {
		return err
	}
	return nil
}

func (a *Agent) handleStart(_ context.Context, _ xconn.Invocation) (xconn.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming {
		return xconn.Result{Args: []any{"already-running"}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.streaming = true
	go a.streamLoop(ctx)
	return xconn.Result{Args: []any{"started"}}, nil
}

func (a *Agent) handleStop(_ context.Context, _ xconn.Invocation) (xconn.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.streaming {
		return xconn.Result{Args: []any{"already-stopped"}}, nil
	}
	a.cancel()
	a.streaming = false
	return xconn.Result{Args: []any{"stopped"}}, nil
}

func (a *Agent) handleMouse(_ context.Context, inv xconn.Invocation) (xconn.Result, error) {
	if len(inv.Args) < 3 {
		return xconn.Result{}, fmt.Errorf("mouse args expected: x, y, button")
	}
	x := toInt(inv.Args[0])
	y := toInt(inv.Args[1])
	button := fmt.Sprintf("%v", inv.Args[2])
	robotgo.Move(x, y)
	if button != "move" {
		robotgo.Click(button, false)
	}
	return xconn.Result{Args: []any{"ok"}}, nil
}

func (a *Agent) handleKey(_ context.Context, inv xconn.Invocation) (xconn.Result, error) {
	if len(inv.Args) < 1 {
		return xconn.Result{}, fmt.Errorf("key arg expected")
	}
	key := fmt.Sprintf("%v", inv.Args[0])
	robotgo.KeyTap(key)
	return xconn.Result{Args: []any{"ok"}}, nil
}

func (a *Agent) streamLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second / time.Duration(a.fps))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame, err := captureFrame(a.frameWidth, a.jpegQuality)
			if err != nil {
				log.Printf("capture error: %v", err)
				continue
			}
			if err := a.client.Publish(topicFrame, frame); err != nil {
				log.Printf("publish error: %v", err)
			}
		}
	}
}

func captureFrame(maxWidth, quality int) ([]byte, error) {
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}

	resized := resizeNearest(img, maxWidth)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func resizeNearest(src image.Image, maxWidth int) image.Image {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= maxWidth {
		return src
	}
	ratio := float64(maxWidth) / float64(w)
	nh := int(float64(h) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, maxWidth, nh))
	for y := 0; y < nh; y++ {
		sy := int(float64(y) / ratio)
		for x := 0; x < maxWidth; x++ {
			sx := int(float64(x) / ratio)
			dst.Set(x, y, src.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

func envOrDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(k string, fallback int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}
