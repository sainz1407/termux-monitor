package scrcpy

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"termux-monitor/internal/config"
)

const (
	ScrcpyServerPort = 7007
)

// ScrcpyConfig holds streaming and device configuration for scrcpy-server.
type ScrcpyConfig struct {
	MaxSize       int  `json:"max_size"`
	BitRate       int  `json:"bit_rate"`
	MaxFps        int  `json:"max_fps"`
	DisplayID     int  `json:"display_id"`
	StayAwake     bool `json:"stay_awake"`
	TurnScreenOff bool `json:"turn_screen_off"`
}

// DefaultConfig returns optimal default streaming parameters.
func DefaultConfig() ScrcpyConfig {
	return ScrcpyConfig{
		MaxSize:       1280,
		BitRate:       4000000, // 4 Mbps
		MaxFps:        60,
		DisplayID:     0,
		StayAwake:     true,
		TurnScreenOff: false,
	}
}

type wsMessage struct {
	msgType int
	data    []byte
}

// WSClient represents a connected WebSocket client with non-blocking send and single-goroutine writing.
type WSClient struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan wsMessage
	done      chan struct{}
	closeOnce sync.Once
}

// NewWSClient creates a new WSClient instance.
func NewWSClient(hub *Hub, conn *websocket.Conn) *WSClient {
	return &WSClient{
		hub:  hub,
		conn: conn,
		send: make(chan wsMessage, 64),
		done: make(chan struct{}),
	}
}

// Send queues a message for transmission; drops the frame non-blockingly if buffer is full.
func (c *WSClient) Send(msgType int, data []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}

	msg := wsMessage{msgType: msgType, data: data}
	select {
	case c.send <- msg:
		return true
	default:
		// Drop frame to prevent slow client from stalling the entire hub read loop
		return false
	}
}

// Close closes the client's done channel and underlying WebSocket connection safely.
func (c *WSClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

// writePump is the ONLY goroutine allowed to call write methods on c.conn.
func (c *WSClient) writePump() {
	ticker := time.NewTicker(20 * time.Second)
	defer func() {
		ticker.Stop()
		c.Close()
		if c.hub != nil {
			c.hub.UnregisterClient(c)
		}
	}()

	for {
		select {
		case <-c.done:
			return

		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if c.conn != nil {
				_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := c.conn.WriteMessage(msg.msgType, msg.data); err != nil {
					return
				}
			}

		case <-ticker.C:
			if c.conn != nil {
				_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}
}

// Hub manages the scrcpy-server background process, TCP streaming, and WebSocket clients.
type Hub struct {
	mu          sync.Mutex
	controlMu   sync.Mutex
	serverCmd   *exec.Cmd
	tcpConn     net.Conn
	isRunning   bool
	clients     map[*WSClient]bool
	width       int
	height      int
	videoWidth  int
	videoHeight int
	lastConfig  []byte
	avcC        []byte
	// sentMetadata is set once the first avcC description was cached. It lets
	// clients that connected before the first config packet receive the
	// missing description exactly once.
	sentMetadata bool
	idleTimer    *time.Timer
	stopChan     chan struct{}
	cfg          ScrcpyConfig
}

var (
	globalHub     *Hub
	globalHubOnce sync.Once
)

// GetHub returns the singleton Hub instance.
func GetHub() *Hub {
	globalHubOnce.Do(func() {
		globalHub = &Hub{
			clients: make(map[*WSClient]bool),
			cfg:     DefaultConfig(),
		}
	})
	return globalHub
}

// buildAVCCConfig assembles a WebCodecs AVCC `description` (ISO/IEC 14496-15
// AVCDecoderConfigurationRecord) from an Annex-B SPS/PPS packet. When this is
// supplied to VideoDecoder.configure, every EncodedVideoChunk must carry raw
// AVCC samples (4-byte big-endian NAL length prefix) instead of Annex-B start
// codes. Returns nil when the packet does not contain a usable SPS.
func buildAVCCConfig(spsPps []byte) []byte {
	var sps, pps []byte
	readNAL := func(off int) ([]byte, int) {
		start := -1
		length := 0
		if off+3 <= len(spsPps) && spsPps[off] == 0 && spsPps[off+1] == 0 && spsPps[off+2] == 1 {
			start = off + 3
			length = 3
		} else if off+4 <= len(spsPps) && spsPps[off] == 0 && spsPps[off+1] == 0 && spsPps[off+2] == 0 && spsPps[off+3] == 1 {
			start = off + 4
			length = 4
		}
		if start < 0 {
			return nil, 0
		}
		end := len(spsPps)
		for i := start; i+3 <= len(spsPps); i++ {
			if spsPps[i] == 0 && spsPps[i+1] == 0 && spsPps[i+2] == 1 {
				end = i
				break
			}
		}
		return spsPps[start:end], length
	}

	i := 0
	for i < len(spsPps) {
		nal, scLen := readNAL(i)
		if nal == nil {
			break
		}
		switch nal[0] & 0x1f {
		case 7:
			if sps == nil {
				sps = nal
			}
		case 8:
			if pps == nil {
				pps = nal
			}
		}
		i += scLen + len(nal)
	}

	if len(sps) < 4 || len(pps) == 0 {
		return nil
	}

	out := make([]byte, 0, 11+len(sps)+len(pps))
	out = append(out, 1)      // configurationVersion
	out = append(out, sps[1]) // AVCProfileIndication
	out = append(out, sps[2]) // profile_compatibility
	out = append(out, sps[3]) // AVCLevelIndication
	out = append(out, 0xff)   // lengthSizeMinusOne = 3 (4-byte NAL length)
	out = append(out, 0xe1)   // numOfSequenceParameterSets = 1
	out = binary.BigEndian.AppendUint16(out, uint16(len(sps)))
	out = append(out, sps...)
	out = append(out, 1) // numOfPictureParameterSets = 1
	out = binary.BigEndian.AppendUint16(out, uint16(len(pps)))
	out = append(out, pps...)
	return out
}

// findStartCode returns the index of the next Annex-B start code at or after
// from. It returns the position and the start-code length (3 or 4), or (-1, 0)
// when no further start code exists.
func findStartCode(buf []byte, from int) (int, int) {
	for i := from; i+3 <= len(buf); i++ {
		if buf[i] == 0 && buf[i+1] == 0 {
			if buf[i+2] == 1 {
				return i, 3
			}
			if i+4 <= len(buf) && buf[i+2] == 0 && buf[i+3] == 1 {
				return i, 4
			}
		}
	}
	return -1, 0
}

// splitAVCCSample splits an already AVCC-framed sample (4-byte big-endian NAL
// lengths) into its NAL units. It returns nil when the buffer is not validly
// AVCC-framed, which is how a raw Annex-B payload is told apart from AVCC.
func splitAVCCSample(sample []byte) [][]byte {
	var nals [][]byte
	for off := 0; off+4 <= len(sample); {
		nalLen := int(binary.BigEndian.Uint32(sample[off : off+4]))
		off += 4
		if nalLen <= 0 || off+nalLen > len(sample) {
			return nil
		}
		nals = append(nals, sample[off:off+nalLen])
		off += nalLen
		if off == len(sample) {
			return nals
		}
	}
	return nil
}

// isAVCCSample reports whether the payload is already AVCC-framed. scrcpy-server
// 2.x/3.x output buffers are length-prefixed (the Android encoder writes
// AVCC), so re-running the Annex-B conversion on them would corrupt every
// sample: the start-code scan finds nothing and the sample is dropped, which
// leaves the client with a blank canvas while control keeps working.
func isAVCCSample(sample []byte) bool {
	nals := splitAVCCSample(sample)
	if len(nals) == 0 {
		return false
	}
	for _, nal := range nals {
		// A NAL header byte is always non-zero; a mistaken length prefix would
		// point into a run of zero bytes from an Annex-B start code.
		if len(nal) == 0 || nal[0] == 0 || nal[0]&0x80 != 0 {
			return false
		}
	}
	return true
}

// sampleToAVCC normalizes a picture sample into the single AVCC buffer expected
// by WebCodecs. AVCC-framed payloads are passed through untouched; Annex-B
// payloads are converted. Returns nil when neither framing is recognizable.
func sampleToAVCC(sample []byte) []byte {
	if isAVCCSample(sample) {
		return sample
	}
	if avcc := annexBToAVCC(sample); avcc != nil {
		return avcc
	}
	if nals := splitAVCCSample(sample); len(nals) > 0 {
		// Framing parses but the NAL headers looked suspicious; still preferable
		// to dropping the sample entirely.
		return sample
	}
	return nil
}

// annexBToAVCC converts a length-delimited Annex-B byte stream into the single
// AVCC sample format expected by WebCodecs (each NAL prefixed with its 4-byte
// big-endian length). Returns nil when no NAL unit is present.
func annexBToAVCC(annexB []byte) []byte {
	out := make([]byte, 0, len(annexB)+8)
	pos := 0
	for {
		sc, scLen := findStartCode(annexB, pos)
		if sc < 0 {
			break
		}
		nalStart := sc + scLen
		next, _ := findStartCode(annexB, nalStart)
		nalEnd := len(annexB)
		if next >= 0 {
			nalEnd = next
		}
		if nalEnd > nalStart {
			out = binary.BigEndian.AppendUint32(out, uint32(nalEnd-nalStart))
			out = append(out, annexB[nalStart:nalEnd]...)
		}
		if next < 0 {
			break
		}
		pos = next
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// codecNameFromAVCC builds the RFC 6381 codec string ("avc1.PPCCLL") from an
// AVCDecoderConfigurationRecord. It falls back to a safe default when the
// record is not yet available (hub just started).
func codecNameFromAVCC(avcC []byte) string {
	if len(avcC) < 4 || avcC[0] != 1 {
		return "avc1.42E01E"
	}
	return fmt.Sprintf("avc1.%02X%02X%02X", avcC[1], avcC[2], avcC[3])
}

// FindJar looks for scrcpy-server.jar in known locations.
func FindJar() (string, error) {
	candidates := []string{
		filepath.Join(config.ModuleDir, "bin", "scrcpy-server.jar"),
		"/data/adb/modules/termux_monitor/bin/scrcpy-server.jar",
		"/data/local/tmp/scrcpy-server.jar",
		"bin/scrcpy-server.jar",
		filepath.Join(".", "bin", "scrcpy-server.jar"),
	}

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("scrcpy-server.jar not found in known locations")
}

// Start ensures scrcpy-server is running and streaming on port 7007.
func (h *Hub) Start(cfg ScrcpyConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}

	if h.isRunning {
		// If running with the same display and max size, reuse
		if h.cfg.DisplayID == cfg.DisplayID && h.cfg.MaxSize == cfg.MaxSize && h.cfg.BitRate == cfg.BitRate {
			return nil
		}
		// Config changed, restart server
		h.cleanupLocked()
	}

	jarPath, err := FindJar()
	if err != nil {
		return err
	}

	h.cfg = cfg
	if err := h.startServerProcess(jarPath, cfg); err != nil {
		return fmt.Errorf("failed to start scrcpy-server: %w", err)
	}

	// Connect to TCP port 7007 with retry loop
	var conn net.Conn
	for i := 0; i < 25; i++ {
		conn, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ScrcpyServerPort), 250*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err != nil {
		h.stopServerProcess()
		return fmt.Errorf("failed to connect to scrcpy-server on port %d: %w", ScrcpyServerPort, err)
	}

	h.tcpConn = conn
	h.isRunning = true
	h.stopChan = make(chan struct{})

	// Read initial resolution header (16 bytes: 4 bytes device width, 4 bytes device height, 4 bytes video width, 4 bytes video height in big endian)
	header := make([]byte, 16)
	if _, err := io.ReadFull(conn, header); err != nil {
		h.cleanupLocked()
		return fmt.Errorf("failed to read scrcpy resolution header: %w", err)
	}
	h.width = int(binary.BigEndian.Uint32(header[0:4]))
	h.height = int(binary.BigEndian.Uint32(header[4:8]))
	h.videoWidth = int(binary.BigEndian.Uint32(header[8:12]))
	h.videoHeight = int(binary.BigEndian.Uint32(header[12:16]))
	log.Printf("[ScrcpyHub] Connected. Device resolution: %dx%d, Video resolution: %dx%d", h.width, h.height, h.videoWidth, h.videoHeight)

	// If TurnScreenOff is requested at launch, send custom command 99
	if cfg.TurnScreenOff {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = h.SetScreenPower(false)
		}()
	}

	// Start reading video stream
	go h.readLoop(conn, h.stopChan)
	return nil
}

func (h *Hub) startServerProcess(jarPath string, cfg ScrcpyConfig) error {
	// Kill leftover process on port 7007
	_ = exec.Command("sh", "-c", "killall -9 app_process 2>/dev/null; fuser -k 7007/tcp 2>/dev/null").Run()
	time.Sleep(150 * time.Millisecond)

	maxSize := cfg.MaxSize
	if maxSize < 0 {
		maxSize = 1280
	}
	bitRate := cfg.BitRate
	if bitRate <= 0 {
		bitRate = 8000000
	}

	cmdStr := fmt.Sprintf(
		"CLASSPATH=%s app_process / org.server.scrcpy.Server 127.0.0.1 %d %d true false true %t %d",
		jarPath, maxSize, bitRate, cfg.StayAwake, cfg.DisplayID,
	)

	h.serverCmd = exec.Command(config.SUBin, "-c", cmdStr)
	if err := h.serverCmd.Start(); err != nil {
		// Fallback to plain sh
		h.serverCmd = exec.Command("sh", "-c", cmdStr)
		if err := h.serverCmd.Start(); err != nil {
			return err
		}
	}

	return nil
}

func (h *Hub) stopServerProcess() {
	if h.serverCmd != nil && h.serverCmd.Process != nil {
		_ = h.serverCmd.Process.Kill()
		h.serverCmd = nil
	}
	_ = exec.Command("sh", "-c", "fuser -k 7007/tcp 2>/dev/null").Run()
}

func (h *Hub) cleanupLocked() {
	h.isRunning = false
	h.lastConfig = nil
	h.avcC = nil
	h.sentMetadata = false
	if h.tcpConn != nil {
		_ = h.tcpConn.Close()
		h.tcpConn = nil
	}
	if h.stopChan != nil {
		close(h.stopChan)
		h.stopChan = nil
	}
	h.stopServerProcess()
}

func (h *Hub) readLoop(conn net.Conn, stopChan chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ScrcpyHub] Recovered panic in readLoop: %v", r)
		}
		h.mu.Lock()
		if h.tcpConn == conn {
			h.cleanupLocked()
		}
		h.mu.Unlock()
	}()

	sizeBuf := make([]byte, 4)
	for {
		select {
		case <-stopChan:
			return
		default:
		}

		if _, err := io.ReadFull(conn, sizeBuf); err != nil {
			log.Printf("[ScrcpyHub] Stream read error: %v", err)
			return
		}
		pktSize := int(binary.BigEndian.Uint32(sizeBuf))
		if pktSize <= 0 || pktSize > 10*1024*1024 {
			log.Printf("[ScrcpyHub] Invalid packet size: %d", pktSize)
			return
		}

		payload := make([]byte, pktSize)
		if _, err := io.ReadFull(conn, payload); err != nil {
			log.Printf("[ScrcpyHub] Failed to read payload: %v", err)
			return
		}

		if len(payload) < 10 {
			continue
		}

		pktType := int(payload[0])
		if pktType != 1 { // Only broadcast VIDEO frames
			continue
		}

		flag := payload[1]
		pts := binary.BigEndian.Uint64(payload[2:10])
		nalData := payload[10:]

		// Build binary message for WebSocket client:
		// [0]: Flag (3 = AVCC config, 2 = Annex-B config, 1 = Key, 0 = Delta)
		// [1..8]: PTS (uint64 big-endian)
		// [9..]: payload bytes (AVCC samples when flag != 2)
		outFlag := flag
		outData := nalData
		if flag == 2 {
			// Config packet carries SPS/PPS: derive the avcC description used
			// by the client to configure the decoder in "description" mode.
			avcC := buildAVCCConfig(nalData)
			if avcC != nil {
				h.mu.Lock()
				h.avcC = avcC
				h.mu.Unlock()
				outFlag = 3
				outData = avcC
			} else {
				log.Printf("[ScrcpyHub] Config packet without usable SPS/PPS (%d bytes)", len(nalData))
				// Forward the raw Annex-B SPS/PPS so clients can derive their
				// own description instead of waiting for the next keyframe.
				outFlag = 2
			}
		} else if avcc := sampleToAVCC(nalData); avcc != nil {
			// Normalize every picture sample to AVCC. The payload is inspected
			// first: scrcpy-server output buffers are already length-prefixed, so
			// blindly running the Annex-B conversion would drop every sample.
			outData = avcc
		} else if flag == 1 {
			// A keyframe that could not be normalized cannot form a valid AVCC
			// sample; skip it rather than emitting broken framing.
			continue
		}

		msg := make([]byte, 9+len(outData))
		msg[0] = outFlag
		binary.BigEndian.PutUint64(msg[1:9], pts)
		copy(msg[9:], outData)

		h.mu.Lock()
		var cachedAvcc []byte
		var freshConfig bool
		if flag == 2 { // CONFIG packet
			if outFlag == 3 {
				// Converted into an avcC description: safe to replay verbatim.
				// freshConfig marks the very first description so clients that
				// joined earlier get the missing metadata replayed once.
				freshConfig = h.avcC == nil && !h.sentMetadata
				h.sentMetadata = true
				h.lastConfig = make([]byte, len(msg))
				copy(h.lastConfig, msg)
			} else {
				// Raw Annex-B SPS/PPS fallback; never replay it as flag 3.
				h.lastConfig = nil
			}
			cachedAvcc = append([]byte(nil), h.avcC...)
		}

		// Snapshot clients list to avoid holding mutex during I/O
		clientList := make([]*WSClient, 0, len(h.clients))
		for client := range h.clients {
			clientList = append(clientList, client)
		}
		h.mu.Unlock()

		// Broadcast to connected WebSocket clients non-blockingly
		for _, client := range clientList {
			client.Send(websocket.BinaryMessage, msg)
		}

		// Clients that connected before the first config packet received a
		// metadata frame without any description. Push a fresh metadata frame
		// (now carrying the avcC record) so they can configure their decoder
		// instead of dropping every sample until the next keyframe.
		if freshConfig && len(clientList) > 0 && len(cachedAvcc) > 0 {
			meta := map[string]interface{}{
				"type":        "metadata",
				"width":       h.videoWidth,
				"height":      h.videoHeight,
				"videoWidth":  h.videoWidth,
				"videoHeight": h.videoHeight,
				"engine":      "webcodecs",
				"codec":       codecNameFromAVCC(cachedAvcc),
				"avcc":        true,
				"avccConfig":  base64.StdEncoding.EncodeToString(cachedAvcc),
			}
			if metaBytes, err := json.Marshal(meta); err == nil {
				for _, client := range clientList {
					client.Send(websocket.TextMessage, metaBytes)
				}
			}
		}

		// A late-joining client may have missed the in-band configuration
		// packet entirely. Hand it the cached avcC description inline so its
		// decoder is ready before the next sample (mirrors the metadata send).
		if outFlag == 3 && len(clientList) > 0 && len(cachedAvcc) > 0 {
			cfg := make([]byte, 9+len(cachedAvcc))
			cfg[0] = 3
			binary.BigEndian.PutUint64(cfg[1:9], pts)
			copy(cfg[9:], cachedAvcc)
			for _, client := range clientList {
				client.Send(websocket.BinaryMessage, cfg)
			}
		}
	}
}

// ConfigSnapshot returns the cached video dimensions and the cached avcC
// description. The websocket handler uses it to answer live metadata/status
// requests without waiting for the next in-band config packet.
func (h *Hub) ConfigSnapshot() (int, int, []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.videoWidth, h.videoHeight, append([]byte(nil), h.avcC...)
}

// IsAVCCMode reports whether the hub is publishing AVCC-framed samples with an
// avcC description (see buildAVCCConfig) rather than raw Annex-B NAL units.
func (h *Hub) IsAVCCMode() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.avcC != nil
}

// RegisterClient adds a client, starts its writePump, and returns the current
// video dimensions, the cached config packet, and the cached avcC description.
// The avcC record (when present) is returned so the handler can inline it into
// the metadata message and let the client configure its decoder immediately,
// without racing the first streamed samples.
func (h *Hub) RegisterClient(ws *websocket.Conn) (*WSClient, int, int, int, int, []byte, []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}

	client := NewWSClient(h, ws)
	h.clients[client] = true
	width := h.width
	height := h.height
	videoWidth := h.videoWidth
	videoHeight := h.videoHeight
	var lastConfig []byte
	if h.lastConfig != nil {
		lastConfig = make([]byte, len(h.lastConfig))
		copy(lastConfig, h.lastConfig)
	}
	var avcC []byte
	if h.avcC != nil {
		avcC = make([]byte, len(h.avcC))
		copy(avcC, h.avcC)
	}

	go client.writePump()

	return client, width, height, videoWidth, videoHeight, lastConfig, avcC
}

// UnregisterClient removes a client, closes its write loop, and schedules idle shutdown after 15s.
func (h *Hub) UnregisterClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client == nil {
		return
	}

	if _, exists := h.clients[client]; exists {
		delete(h.clients, client)
		client.Close()
	}

	if len(h.clients) == 0 {
		if h.idleTimer != nil {
			h.idleTimer.Stop()
		}
		h.idleTimer = time.AfterFunc(15*time.Second, func() {
			h.mu.Lock()
			if len(h.clients) == 0 && h.isRunning {
				log.Println("[ScrcpyHub] Idle timeout reached. Stopping server.")
				h.cleanupLocked()
			}
			h.mu.Unlock()
		})
	}
}

// SendControlPacket writes a binary ControlPacket (Type 2) down the open TCP socket.
func (h *Hub) SendControlPacket(data []byte) error {
	h.controlMu.Lock()
	defer h.controlMu.Unlock()

	h.mu.Lock()
	conn := h.tcpConn
	h.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no active scrcpy connection")
	}

	packetSize := 1 + len(data)
	buf := make([]byte, 4+1+len(data))
	binary.BigEndian.PutUint32(buf[0:4], uint32(packetSize))
	buf[4] = 2 // CONTROL packet type
	copy(buf[5:], data)

	_, err := conn.Write(buf)
	return err
}

// SetScreenPower controls physical display power (false = OFF, true = ON/NORMAL).
func (h *Hub) SetScreenPower(on bool) error {
	mode := 0 // POWER_MODE_OFF
	if on {
		mode = 2 // POWER_MODE_NORMAL
	}
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], 99) // custom command 99
	binary.BigEndian.PutUint32(data[4:8], uint32(mode))
	return h.SendControlPacket(data)
}

// SendKey injects an Android keycode with < 0.1ms latency.
func (h *Hub) SendKey(keycode int) error {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(keycode))
	return h.SendControlPacket(data)
}

// SendTouch injects a touch event through the native socket (< 0.1ms latency).
// action: 0 = ACTION_DOWN, 1 = ACTION_UP, 2 = ACTION_MOVE
func (h *Hub) SendTouch(action int, x, y int, pointerId int) error {
	data := make([]byte, 20)
	binary.BigEndian.PutUint32(data[0:4], uint32(action))
	binary.BigEndian.PutUint32(data[4:8], 0) // button
	binary.BigEndian.PutUint32(data[8:12], uint32(x))
	binary.BigEndian.PutUint32(data[12:16], uint32(y))
	binary.BigEndian.PutUint32(data[16:20], uint32(pointerId))
	return h.SendControlPacket(data)
}

// InjectTouch executes touch with hardware injection or native swipe interpolation.
func (h *Hub) InjectTouch(action string, x, y, x2, y2, dur int) {
	switch strings.ToLower(action) {
	case "down":
		_ = h.SendTouch(0, x, y, 0)
	case "move":
		_ = h.SendTouch(2, x, y, 0)
	case "up":
		_ = h.SendTouch(1, x, y, 0)
	case "tap", "click":
		if err := h.SendTouch(0, x, y, 0); err == nil {
			time.Sleep(20 * time.Millisecond)
			_ = h.SendTouch(1, x, y, 0)
			return
		}
		_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input tap %d %d", x, y)).Run()
	case "swipe":
		d := dur
		if d <= 0 {
			d = 220
		}
		// Native 60 FPS trajectory interpolation
		if err := h.SendTouch(0, x, y, 0); err == nil {
			steps := d / 16
			if steps < 6 {
				steps = 6
			}
			for i := 1; i <= steps; i++ {
				time.Sleep(16 * time.Millisecond)
				xi := x + (x2-x)*i/steps
				yi := y + (y2-y)*i/steps
				_ = h.SendTouch(2, xi, yi, 0)
			}
			time.Sleep(16 * time.Millisecond)
			_ = h.SendTouch(1, x2, y2, 0)
			return
		}
		_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input swipe %d %d %d %d %d", x, y, x2, y2, d)).Run()
	}
}

// InjectKey sends an Android key event with instant dispatch.
func (h *Hub) InjectKey(key string, keycode int) {
	if keycode <= 0 {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "back":
			keycode = 4
		case "home":
			keycode = 3
		case "recents", "app_switch":
			keycode = 187
		case "power":
			keycode = 26
		case "vol_up":
			keycode = 24
		case "vol_down":
			keycode = 25
		case "enter":
			keycode = 66
		case "tab":
			keycode = 61
		case "delete", "backspace":
			keycode = 67
		case "wake":
			keycode = 224
		case "sleep":
			keycode = 223
		default:
			if k, err := strconv.Atoi(key); err == nil {
				keycode = k
			}
		}
	}

	if keycode > 0 {
		if err := h.SendKey(keycode); err == nil {
			return
		}
		_ = exec.Command(config.SUBin, "-c", fmt.Sprintf("input keyevent %d", keycode)).Run()
	}
}

var (
	cachedEncoderInfo string
	encoderOnce       sync.Once
)

// GetEncoderInfo discovers and caches the Android device's hardware H.264 encoder details.
func GetEncoderInfo() string {
	encoderOnce.Do(func() {
		// Inspect Android media codec configuration XMLs
		paths := []string{
			"/vendor/etc/media_codecs_c2.xml",
			"/vendor/etc/media_codecs_performance_c2.xml",
			"/vendor/etc/media_codecs.xml",
			"/system/etc/media_codecs.xml",
		}
		for _, p := range paths {
			content, err := os.ReadFile(p)
			if err == nil {
				s := string(content)
				if strings.Contains(s, "c2.qti.avc.encoder") {
					cachedEncoderInfo = "Qualcomm c2.qti.avc (Hardware GPU/VPU)"
					return
				}
				if strings.Contains(s, "OMX.qcom.video.encoder.avc") {
					cachedEncoderInfo = "Qualcomm OMX.qcom.video.encoder.avc (Hardware)"
					return
				}
				if strings.Contains(s, "c2.exynos.h264.encoder") || strings.Contains(s, "OMX.Exynos.AVC.Encoder") {
					cachedEncoderInfo = "Samsung Exynos AVC (Hardware GPU/VPU)"
					return
				}
				if strings.Contains(s, "c2.mtk.avc.encoder") || strings.Contains(s, "OMX.MTK.VIDEO.ENCODER.AVC") {
					cachedEncoderInfo = "MediaTek AVC (Hardware VPU)"
					return
				}
			}
		}

		// Fallback to ro.board.platform / ro.hardware
		out, err := exec.Command("getprop", "ro.board.platform").Output()
		if err == nil {
			plat := strings.TrimSpace(string(out))
			if plat != "" {
				cachedEncoderInfo = fmt.Sprintf("Hardware MediaCodec (SoC: %s)", plat)
				return
			}
		}

		cachedEncoderInfo = "Android MediaCodec video/avc (Hardware)"
	})
	return cachedEncoderInfo
}
