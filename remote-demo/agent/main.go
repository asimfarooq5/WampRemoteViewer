package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/kbinani/screenshot"
	"github.com/xconnio/xconn-go"
)

const (
	defaultRealm = "default"
	defaultURL   = "ws://localhost:8080/ws"

	procStart  = "io.xconn.desktop.start"
	procStop   = "io.xconn.desktop.stop"
	procMouse  = "io.xconn.desktop.mouse"
	procKey    = "io.xconn.desktop.key"
	topicFrame = "io.xconn.desktop.frame"
)

type agent struct {
	session     *xconn.Session
	mu          sync.Mutex
	streaming   bool
	cancel      context.CancelFunc
	frameWidth  int
	jpegQuality int
	fps         int
}

func main() {
	url := envOrDefault("WAMP_URL", defaultURL)
	realm := envOrDefault("WAMP_REALM", defaultRealm)

	client := xconn.Client{}
	session, err := client.Connect(context.Background(), url, realm)
	if err != nil {
		log.Fatalf("wamp connect failed: %v", err)
	}
	defer session.Close()

	a := &agent{
		session:     session,
		frameWidth:  envIntOrDefault("FRAME_WIDTH", 1280),
		jpegQuality: envIntOrDefault("JPEG_QUALITY", 60),
		fps:         envIntOrDefault("FPS", 10),
	}

	if err := a.registerProcedures(); err != nil {
		log.Fatalf("register procedures failed: %v", err)
	}

	log.Printf("agent connected: %s (%s)", url, realm)
	waitForSignal(a)
}

func (a *agent) registerProcedures() error {
	type rpcDef struct {
		name    string
		handler func(*xconn.Invocation) ([]any, error)
	}

	rpcs := []rpcDef{
		{name: procStart, handler: a.rpcStart},
		{name: procStop, handler: a.rpcStop},
		{name: procMouse, handler: a.rpcMouse},
		{name: procKey, handler: a.rpcKey},
	}

	for _, rpc := range rpcs {
		res := a.session.Register(rpc.name).Endpoint(func(inv *xconn.Invocation) *xconn.InvocationResult {
			args, err := rpc.handler(inv)
			if err != nil {
				return xconn.NewInvocationError(err)
			}
			return xconn.NewInvocationResult(args...)
		}).Do()
		if res.Err != nil {
			return fmt.Errorf("register %s: %w", rpc.name, res.Err)
		}
	}

	return nil
}

func (a *agent) rpcStart(_ *xconn.Invocation) ([]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.streaming {
		return []any{"already-running"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.streaming = true
	go a.streamLoop(ctx)
	return []any{"started"}, nil
}

func (a *agent) rpcStop(_ *xconn.Invocation) ([]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.streaming {
		return []any{"already-stopped"}, nil
	}

	a.cancel()
	a.cancel = nil
	a.streaming = false
	return []any{"stopped"}, nil
}

func (a *agent) rpcMouse(inv *xconn.Invocation) ([]any, error) {
	if len(inv.Args) < 3 {
		return nil, fmt.Errorf("mouse args expected: x, y, button")
	}

	x, err := toInt(inv.Args[0])
	if err != nil {
		return nil, fmt.Errorf("x parse error: %w", err)
	}
	y, err := toInt(inv.Args[1])
	if err != nil {
		return nil, fmt.Errorf("y parse error: %w", err)
	}
	button := fmt.Sprintf("%v", inv.Args[2])

	robotgo.Move(x, y)
	if button != "move" {
		robotgo.Click(button, false)
	}
	return []any{"ok"}, nil
}

func (a *agent) rpcKey(inv *xconn.Invocation) ([]any, error) {
	if len(inv.Args) < 1 {
		return nil, fmt.Errorf("key arg expected")
	}
	key := fmt.Sprintf("%v", inv.Args[0])
	if key == "" {
		return nil, fmt.Errorf("empty key")
	}
	robotgo.KeyTap(key)
	return []any{"ok"}, nil
}

func (a *agent) streamLoop(ctx context.Context) {
	interval := time.Second / time.Duration(a.fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame, err := captureFrame(a.frameWidth, a.jpegQuality)
			if err != nil {
				log.Printf("capture frame error: %v", err)
				continue
			}

			publish := a.session.Publish(topicFrame).Args(frame).Do()
			if publish.Err != nil {
				log.Printf("publish frame error: %v", publish.Err)
			}
		}
	}
}

func captureFrame(maxWidth, quality int) ([]byte, error) {
	displays := screenshot.NumActiveDisplays()
	if displays == 0 {
		return nil, fmt.Errorf("no active display")
	}

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
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxWidth <= 0 || w <= maxWidth {
		return src
	}

	ratio := float64(maxWidth) / float64(w)
	nh := int(float64(h) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, maxWidth, nh))

	for y := 0; y < nh; y++ {
		sy := int(float64(y) / ratio)
		for x := 0; x < maxWidth; x++ {
			sx := int(float64(x) / ratio)
			dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

func waitForSignal(a *agent) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	a.mu.Lock()
	if a.streaming && a.cancel != nil {
		a.cancel()
		a.streaming = false
	}
	a.mu.Unlock()
}

func envOrDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(k string, fallback int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func toInt(v any) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		return int(x), nil
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}
