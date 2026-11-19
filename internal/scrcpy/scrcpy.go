package scrcpy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"termux-monitor/internal/bufferpool"
	"termux-monitor/internal/config"
)

type InputEvent struct {
	Action    string `json:"action"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	X2        int    `json:"x2"`
	Y2        int    `json:"y2"`
	Duration  int    `json:"duration"`
	Text      string `json:"text"`
	KeyCode   int    `json:"keycode"`
	PointerID int    `json:"pointerId"`
	On        bool   `json:"on"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
	},
}

func captureScreenFrame() ([]byte, error) {
	out, err := exec.Command(config.SUBin, "-c", "screencap -p").Output()
	if err != nil || len(out) == 0 {
		out, err = exec.Command("/system/bin/screencap", "-p").Output()
		if err != nil || len(out) == 0 {
			return nil, fmt.Errorf("screencap failed: %v", err)
		}
	}

	img, err := png.Decode(bytes.NewReader(out))
	if err == nil {
		buf := bufferpool.GetBuffer()
		defer bufferpool.PutBuffer(buf)
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 50}); err == nil {
			encoded := make([]byte, buf.Len())
			copy(encoded, buf.Bytes())
			return encoded, nil
		}
	}

	return out, nil
}

// M-13: SanitizeInput cleans shell special characters like backtick, dollar sign,
// semicolon, backslash, quotes, and other shell operators from text input.
func SanitizeInput(text string) string {
	if text == "" {
		return ""
	}
	s := strings.ReplaceAll(text, " ", "%s")
	replacer := strings.NewReplacer(
		"`", "",
		"$", "",
		";", "",
		"\\", "",
		"'", "",
		"\"", "",
		"&", "",
		"|", "",
		"<", "",
		">", "",
		"(", "",
		")", "",
		"{", "",
		"}", "",
		"\n", "",
		"\r", "",
	)
	return replacer.Replace(s)
}

func handleInputEvent(evt InputEvent) {
	switch evt.Action {
	case "click", "tap":
		if evt.X >= 0 && evt.Y >= 0 {
			_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input tap %d %d", evt.X, evt.Y)).Run()
		}
	case "swipe":
		dur := evt.Duration
		if dur <= 0 {
			dur = 300
		}
		_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input swipe %d %d %d %d %d", evt.X, evt.Y, evt.X2, evt.Y2, dur)).Run()
	case "key", "keycode":
		if evt.KeyCode > 0 {
			_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input keyevent %d", evt.KeyCode)).Run()
		}
	case "text":
		safeText := SanitizeInput(evt.Text)
		if safeText != "" {
			_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input text '%s'", safeText)).Run()
		}
	case "back":
		_ = exec.Command(config.SUBin, "-c", "input keyevent 4").Run()
	case "home":
		_ = exec.Command(config.SUBin, "-c", "input keyevent 3").Run()
	case "recents", "app_switch":
		_ = exec.Command(config.SUBin, "-c", "input keyevent 187").Run()
	case "vol_up":
		_ = exec.Command(config.SUBin, "-c", "input keyevent 24").Run()
	case "vol_down":
		_ = exec.Command(config.SUBin, "-c", "input keyevent 25").Run()
	}
}

// legacyTouchState turns the raw pointer stream (down/move/up) used by the
// browser into the tap/swipe commands understood by the Android `input`
// tool, which is the only injection channel available without the scrcpy
// control socket.
type legacyTouchState struct {
	mu         sync.Mutex
	active     bool
	startX     int
	startY     int
	lastX      int
	lastY      int
	swiped     bool
	swipeFromX int
	swipeFromY int
	run        func(cmd string)
}

func (t *legacyTouchState) execSwipe(fromX, fromY, toX, toY, dur int) {
	t.exec(fmt.Sprintf("input swipe %d %d %d %d %d", fromX, fromY, toX, toY, dur))
}

// exec runs an Android input command. The runner is injectable so the
// gesture translation can be unit tested without touching the device.
func (t *legacyTouchState) exec(cmd string) {
	if t.run != nil {
		t.run(cmd)
		return
	}
	_ = exec.Command(config.SUBin, "-c", cmd).Run()
}

func (t *legacyTouchState) handle(evt InputEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch evt.Action {
	case "down":
		t.active = true
		t.swiped = false
		t.startX, t.startY = evt.X, evt.Y
		t.lastX, t.lastY = evt.X, evt.Y
		t.swipeFromX, t.swipeFromY = evt.X, evt.Y
	case "move":
		if !t.active {
			return
		}
		t.lastX, t.lastY = evt.X, evt.Y
		if !t.swiped {
			if abs(evt.X-t.startX) > 12 || abs(evt.Y-t.startY) > 12 {
				t.swiped = true
				t.swipeFromX, t.swipeFromY = evt.X, evt.Y
				t.execSwipe(t.startX, t.startY, evt.X, evt.Y, 250)
			}
			return
		}
		if abs(evt.X-t.swipeFromX) > 24 || abs(evt.Y-t.swipeFromY) > 24 {
			t.execSwipe(t.swipeFromX, t.swipeFromY, evt.X, evt.Y, 200)
			t.swipeFromX, t.swipeFromY = evt.X, evt.Y
		}
	case "up":
		if !t.active {
			return
		}
		t.active = false
		if !t.swiped {
			t.exec(fmt.Sprintf("input tap %d %d", evt.X, evt.Y))
			return
		}
		if abs(evt.X-t.swipeFromX) > 8 || abs(evt.Y-t.swipeFromY) > 8 {
			t.execSwipe(t.swipeFromX, t.swipeFromY, evt.X, evt.Y, 150)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func handleHubInput(hub *Hub, evt InputEvent) {
	switch evt.Action {
	case "down":
		_ = hub.SendTouch(0, evt.X, evt.Y, 0)
	case "move":
		_ = hub.SendTouch(2, evt.X, evt.Y, 0)
	case "up":
		_ = hub.SendTouch(1, evt.X, evt.Y, 0)
	case "tap", "click":
		hub.InjectTouch("tap", evt.X, evt.Y, 0, 0, 0)
	case "swipe":
		hub.InjectTouch("swipe", evt.X, evt.Y, evt.X2, evt.Y2, evt.Duration)
	case "key", "keycode":
		hub.InjectKey("", evt.KeyCode)
	case "back", "home", "recents", "app_switch", "power", "vol_up", "vol_down", "enter", "tab", "delete", "backspace", "wake", "sleep":
		hub.InjectKey(evt.Action, 0)
	case "screen_power":
		_ = hub.SetScreenPower(evt.On)
	case "screen_power_off":
		_ = hub.SetScreenPower(false)
	case "screen_power_on":
		_ = hub.SetScreenPower(true)
	case "text":
		safeText := SanitizeInput(evt.Text)
		if safeText != "" {
			_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input text '%s'", safeText)).Run()
		}
	case "resync":
		// Handled by the websocket read loop, which replies on the connection.
	}
}

// sendStreamMetadata emits a metadata text frame carrying the current stream
// description (dimensions + inline avcC record). It is used right after the
// hub starts, since the first connection often happens before any config
// packet has been observed.
func sendStreamMetadata(hub *Hub, send func(int, []byte) bool) {
	videoWidth, videoHeight, avcC := hub.ConfigSnapshot()
	fields := map[string]interface{}{
		"type":        "metadata",
		"width":       videoWidth,
		"height":      videoHeight,
		"videoWidth":  videoWidth,
		"videoHeight": videoHeight,
		"engine":      "webcodecs",
		"codec":       codecNameFromAVCC(avcC),
		"avcc":        true,
	}
	if len(avcC) > 0 {
		fields["avccConfig"] = base64.StdEncoding.EncodeToString(avcC)
	}
	meta, err := json.Marshal(fields)
	if err != nil {
		return
	}
	send(websocket.TextMessage, meta)
}

// Stream engines selectable by the client through the "engine" query
// parameter.
//
// "avcc"  - hardware encoded H.264 delivered as AVCC samples for WebCodecs.
// "mjpeg" - screencap based JPEG frames, used when the browser cannot decode
//
//	H.264 (WebCodecs is only exposed in secure contexts, so any
//	client reaching the dashboard over plain HTTP on a LAN address
//	has no VideoDecoder at all).
const (
	engineAVCC  = "avcc"
	engineMJPEG = "mjpeg"
)

// requestedEngine returns the stream engine requested by the client.
func requestedEngine(r *http.Request) string {
	if strings.EqualFold(r.URL.Query().Get("engine"), engineMJPEG) {
		return engineMJPEG
	}
	return engineAVCC
}

func parseQueryConfig(r *http.Request) ScrcpyConfig {
	cfg := DefaultConfig()
	q := r.URL.Query()

	if q.Has("max_size") {
		if v, err := strconv.Atoi(q.Get("max_size")); err == nil && v >= 0 {
			cfg.MaxSize = v
		}
	}
	if q.Has("bit_rate") {
		if v, err := strconv.Atoi(q.Get("bit_rate")); err == nil && v > 0 {
			cfg.BitRate = v
		}
	}
	if v, err := strconv.Atoi(q.Get("max_fps")); err == nil && v > 0 {
		cfg.MaxFps = v
	}
	if v, err := strconv.Atoi(q.Get("display_id")); err == nil && v >= 0 {
		cfg.DisplayID = v
	}
	if q.Get("stay_awake") == "false" {
		cfg.StayAwake = false
	}
	if q.Get("screen_off") == "true" {
		cfg.TurnScreenOff = true
	}
	return cfg
}

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Scrcpy WS upgrade error: %v", err)
		return
	}
	defer conn.Close()

	const pongWait = 60 * time.Second
	conn.SetReadLimit(512 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	cfg := parseQueryConfig(r)

	// The client explicitly asks for the screencap stream when its browser
	// cannot decode H.264 (no WebCodecs outside of secure contexts).
	if requestedEngine(r) == engineMJPEG {
		log.Printf("[Scrcpy] Client requested the MJPEG screencap stream")
		streamScreencapFrames(conn)
		return
	}

	hub := GetHub()
	hubErr := hub.Start(cfg)

	if hubErr == nil {
		client, width, height, videoWidth, videoHeight, lastConfig, avcC := hub.RegisterClient(conn)
		defer func() {
			hub.UnregisterClient(client)
			client.Close()
		}()

		// The hub always emits AVCC-framed samples (4-byte length-prefixed NALs)
		// and a flag-3 avcC description packet, so the client must always run in
		// description mode. Report avcC support unconditionally instead of
		// probing hub state, which is empty on a freshly started/idle hub and
		// previously caused the browser to fall back to the wrong framing.
		metaFields := map[string]interface{}{
			"type":        "metadata",
			"width":       width,
			"height":      height,
			"videoWidth":  videoWidth,
			"videoHeight": videoHeight,
			"fps":         cfg.MaxFps,
			"engine":      "webcodecs",
			"codec":       codecNameFromAVCC(avcC),
			"avcc":        true,
			"max_size":    cfg.MaxSize,
			"bit_rate":    cfg.BitRate,
			"displayId":   cfg.DisplayID,
			"encoder":     GetEncoderInfo(),
		}
		// Inline the cached avcC description (base64) when the hub already has it
		// so the client can configure its decoder before any sample arrives.
		if len(avcC) > 0 {
			metaFields["avccConfig"] = base64.StdEncoding.EncodeToString(avcC)
		}
		// With no scrcpy output yet the screens have no real dimensions, so use
		// a portrait placeholder instead of a landscape canvas.
		if metaFields["videoWidth"].(int) <= 0 || metaFields["videoHeight"].(int) <= 0 {
			metaFields["width"] = 720
			metaFields["height"] = 1600
			metaFields["videoWidth"] = 720
			metaFields["videoHeight"] = 1600
		}
		meta, _ := json.Marshal(metaFields)
		client.Send(websocket.TextMessage, meta)

		// Send the cached config packet immediately to warm the browser decoder.
		// It already carries flag 3 (avcC) when the hub built one.
		if len(lastConfig) > 0 {
			client.Send(websocket.BinaryMessage, lastConfig)
		}

		// Input event loop
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var evt InputEvent
			if err := json.Unmarshal(msgBytes, &evt); err == nil {
				if evt.Action == "resync" {
					// Client failed to configure its decoder (e.g. it joined
					// before the first config packet): replay the stream
					// description plus the cached avcC packet.
					sendStreamMetadata(hub, client.Send)
					if _, _, avcC := hub.ConfigSnapshot(); len(avcC) > 0 {
						cfg := make([]byte, 9+len(avcC))
						cfg[0] = 3
						copy(cfg[9:], avcC)
						client.Send(websocket.BinaryMessage, cfg)
					}
					continue
				}
				handleHubInput(hub, evt)
			}
		}
		return
	}

	// Fallback to legacy screencap stream if scrcpy-server is unavailable
	log.Printf("[Scrcpy] Hub start failed (%v), falling back to legacy screencap stream", hubErr)
	streamScreencapFrames(conn)
}

// streamScreencapFrames serves the device screen as a low frame rate JPEG
// stream and keeps the legacy touch/keyboard injection path alive.
func streamScreencapFrames(conn *websocket.Conn) {
	fallbackMeta, _ := json.Marshal(map[string]interface{}{
		"type":   "metadata",
		"width":  1080,
		"height": 2400,
		"fps":    3,
		"engine": engineMJPEG,
	})
	_ = conn.WriteMessage(websocket.TextMessage, fallbackMeta)

	done := make(chan struct{})
	var writeMux sync.Mutex
	touch := &legacyTouchState{}

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Scrcpy] Recovered panic in fallback input reader: %v", r)
			}
		}()
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var evt InputEvent
			if err := json.Unmarshal(msgBytes, &evt); err == nil {
				switch evt.Action {
				case "down", "move", "up":
					// Pointer gestures must keep their order, so they are
					// replayed synchronously as tap/swipe commands.
					touch.handle(evt)
				default:
					go handleInputEvent(evt)
				}
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Scrcpy] Recovered panic in fallback screencap loop: %v", r)
		}
	}()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			writeMux.Lock()
			frameData, err := captureScreenFrame()
			if err == nil && len(frameData) > 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
				err = conn.WriteMessage(websocket.BinaryMessage, frameData)
			}
			writeMux.Unlock()

			if err != nil {
				return
			}
		}
	}
}
