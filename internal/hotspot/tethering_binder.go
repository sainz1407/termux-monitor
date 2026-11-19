package hotspot

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"termux-monitor/internal/config"
	"termux-monitor/internal/logger"
)

// Android 8 to Android 10 expose no shell surface for tethering: "cmd wifi
// start-softap" only exists from Android 12 on and the "tethering" service
// only from Android 11 on. On those releases the SoftAP is toggled through the
// connectivity service binder instead.
//
// Vendor ROMs insert their own methods into IConnectivityManager, so the
// transaction codes differ per device and cannot be hardcoded. They are
// discovered at runtime instead: getTetherableIfaces always sits right after
// stopTethering and getTetherableWifiRegexs a few entries later, so both
// anchors lead back to the start and stop transactions.

// tetheringCallerPkg is the package name reported to the connectivity service.
// The framework only checks that it is not null and that the calling UID holds
// the tethering permission, which root always has.
const tetheringCallerPkg = "com.android.shell"

// Legacy ConnectivityManager tethering types, used up to Android 10.
const (
	tetherTypeWifi      = 0
	tetherTypeUSB       = 1
	tetherTypeBluetooth = 2
)

// Transaction codes probed on the connectivity service. The tethering block
// sits inside this range on every known release and vendor ROM.
const (
	binderProbeFirst = 15
	binderProbeLast  = 48
)

// replyExceptionMarker is the first word of a failed binder reply.
const replyExceptionMarker = 0xFFFFFCFF

var (
	reParcelOffset = regexp.MustCompile(`0x[0-9a-fA-F]{8}:`)
	reParcelQuoted = regexp.MustCompile(`'[^']*'`)
	reTetherIface  = regexp.MustCompile(`^(wlan|ap|softap|swlan|eth|usb|rndis|ncm)[0-9]*$`)
)

// tetheringBinderCodes holds the discovered binder transaction codes.
type tetheringBinderCodes struct {
	Start int `json:"start"`
	Stop  int `json:"stop"`
}

var (
	tetheringCodesMu sync.Mutex
	tetheringCodes   *tetheringBinderCodes
)

// getTetheringBinderCodes returns the transaction codes of startTethering and
// stopTethering, discovering them on first use and caching the result.
func getTetheringBinderCodes() (*tetheringBinderCodes, error) {
	tetheringCodesMu.Lock()
	defer tetheringCodesMu.Unlock()

	if tetheringCodes != nil {
		return tetheringCodes, nil
	}

	if cached := loadTetheringCodesCache(); cached != nil {
		tetheringCodes = cached
		return cached, nil
	}

	codes, err := discoverTetheringBinderCodes()
	if err != nil {
		return nil, err
	}

	tetheringCodes = &codes
	saveTetheringCodesCache(codes)
	logger.Get().Infof("hotspot", "discovered tethering transactions: start=%d stop=%d", codes.Start, codes.Stop)
	return tetheringCodes, nil
}

// discoverTetheringBinderCodes probes the connectivity service and locates the
// tethering transactions through the tetherable interface lists.
func discoverTetheringBinderCodes() (tetheringBinderCodes, error) {
	replies := probeConnectivityTransactions()
	if len(replies) == 0 {
		return tetheringBinderCodes{}, fmt.Errorf("the connectivity service did not answer the transaction probe")
	}

	return deriveTetheringBinderCodes(replies)
}

// deriveTetheringBinderCodes locates the start and stop transactions inside
// the replies of the probed transaction codes.
func deriveTetheringBinderCodes(replies map[int][]uint32) (tetheringBinderCodes, error) {
	ifaceAnchor := -1
	regexAnchor := -1
	for code, words := range replies {
		values, ok := decodeReplyStringArray(words)
		if !ok || len(values) == 0 {
			continue
		}

		allIfaces := true
		for _, value := range values {
			if !reTetherIface.MatchString(value) {
				allIfaces = false
				break
			}
		}
		if allIfaces {
			// getTetherableIfaces comes before getTetheredIfaces, so the
			// lowest matching code is the anchor.
			if ifaceAnchor < 0 || code < ifaceAnchor {
				ifaceAnchor = code
			}
			continue
		}

		allRegexps := true
		for _, value := range values {
			if !strings.Contains(value, "\\") {
				allRegexps = false
				break
			}
		}
		if allRegexps && strings.Contains(strings.Join(values, " "), "wlan") {
			if regexAnchor < 0 || code < regexAnchor {
				regexAnchor = code
			}
		}
	}

	start := -1
	if ifaceAnchor > 0 {
		start = ifaceAnchor - 2
	}
	if regexAnchor > 0 {
		alternative := regexAnchor - 7
		if start < 0 {
			start = alternative
		}
	}

	if start <= 0 || start+1 > binderProbeLast {
		return tetheringBinderCodes{}, fmt.Errorf("unable to locate the tethering transactions of the connectivity service")
	}
	return tetheringBinderCodes{Start: start, Stop: start + 1}, nil
}

// probeConnectivityTransactions calls the connectivity service once per
// candidate transaction code and returns the raw reply words of every call.
func probeConnectivityTransactions() map[int][]uint32 {
	var script strings.Builder
	script.WriteString("for i in $(seq ")
	script.WriteString(strconv.Itoa(binderProbeFirst))
	script.WriteString(" ")
	script.WriteString(strconv.Itoa(binderProbeLast))
	script.WriteString("); do echo \"@@$i\"; service call connectivity $i 2>&1; done")

	out, err := config.ExecSuTimeout(30*time.Second, script.String())
	if err != nil && len(out) == 0 {
		logger.Get().Warnf("hotspot", "connectivity transaction probe failed: %v", err)
		return nil
	}
	return parseTransactionReplies(string(out))
}

// parseTransactionReplies splits the probe output into the reply of every
// probed transaction code.
func parseTransactionReplies(out string) map[int][]uint32 {
	replies := make(map[int][]uint32)

	code := -1
	for block := range strings.SplitSeq(out, "@@") {
		header, _, _ := strings.Cut(block, "\n")
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		parsed, err := strconv.Atoi(header)
		if err != nil {
			continue
		}
		code = parsed
		if words := parseParcelWords(block); len(words) > 0 {
			replies[code] = words
		}
	}

	return replies
}

// parseParcelWords extracts the words of a "service call" reply parcel.
func parseParcelWords(block string) []uint32 {
	_, body, found := strings.Cut(block, "Parcel(")
	if !found {
		return nil
	}
	if end := strings.Index(body, ")"); end >= 0 {
		body = body[:end]
	}

	body = reParcelQuoted.ReplaceAllString(body, " ")
	body = reParcelOffset.ReplaceAllString(body, " ")

	var words []uint32
	for token := range strings.FieldsSeq(body) {
		if len(token) != 8 {
			continue
		}
		value, err := strconv.ParseUint(token, 16, 32)
		if err != nil {
			continue
		}
		words = append(words, uint32(value))
	}
	return words
}

// decodeReplyStringArray decodes the string list of a successful reply parcel.
func decodeReplyStringArray(words []uint32) ([]string, bool) {
	if len(words) < 2 || words[0] != 0 {
		return nil, false
	}

	count := int(words[1])
	if count <= 0 || count > 32 {
		return nil, false
	}

	values := make([]string, 0, count)
	position := 2
	for range count {
		if position >= len(words) {
			return nil, false
		}
		length := int(words[position])
		position++
		if length < 0 || length > 1024 {
			return nil, false
		}
		needed := (length + 1) / 2
		if position+needed > len(words) {
			return nil, false
		}
		values = append(values, decodeUTF16(words[position:position+needed], length))
		position += needed
	}
	return values, true
}

// decodeUTF16 turns the packed UTF-16 words of a parcel string into text. The
// parcel pads every string to a whole number of words, so only the declared
// number of characters is taken from the decoded text.
func decodeUTF16(words []uint32, characters int) string {
	raw := make([]byte, len(words)*4)
	for i, word := range words {
		raw[i*4] = byte(word)
		raw[i*4+1] = byte(word >> 8)
		raw[i*4+2] = byte(word >> 16)
		raw[i*4+3] = byte(word >> 24)
	}

	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	if characters >= 0 && characters < len(units) {
		units = units[:characters]
	}
	return string(utf16.Decode(units))
}

// replyFailure returns the error message of a failed binder reply.
func replyFailure(words []uint32) string {
	if len(words) == 0 || words[0] != replyExceptionMarker {
		return ""
	}
	if message := decodeReplyMessage(words); message != "" {
		return message
	}
	return "the connectivity service rejected the request"
}

// decodeReplyMessage decodes the message of a failed reply parcel. The
// framework stores the message as plain bytes, older releases use UTF-16.
func decodeReplyMessage(words []uint32) string {
	if len(words) < 2 {
		return ""
	}
	length := int(words[1])
	if length <= 0 || length > 1024 {
		return ""
	}

	raw := make([]byte, 0, (len(words)-2)*4)
	for _, word := range words[2:] {
		raw = append(raw, byte(word), byte(word>>8), byte(word>>16), byte(word>>24))
	}
	if isUTF16Bytes(raw) {
		units := make([]uint16, 0, len(raw)/2)
		for i := 0; i+1 < len(raw); i += 2 {
			units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
		}
		message := string(utf16.Decode(units))
		if runes := []rune(message); length < len(runes) {
			message = string(runes[:length])
		}
		return message
	}
	if len(raw) > length {
		raw = raw[:length]
	}
	return string(raw)
}

// isUTF16Bytes reports whether the bytes hold a UTF-16LE text.
func isUTF16Bytes(raw []byte) bool {
	if len(raw) < 2 || len(raw)%2 != 0 {
		return false
	}
	for i := 1; i < len(raw); i += 2 {
		if raw[i] != 0 {
			return false
		}
	}
	return true
}

// callConnectivityTransaction performs one binder call on the connectivity
// service and reports the failure of the framework when it refuses the call.
func callConnectivityTransaction(code int, args string) error {
	cmd := fmt.Sprintf("service call connectivity %d%s", code, args)
	out, err := config.ExecSuTimeout(10*time.Second, cmd)

	text := string(out)
	if err != nil && !strings.Contains(text, "Parcel(") {
		return fmt.Errorf("connectivity service call failed: %w (%s)", err, strings.TrimSpace(text))
	}

	if message := replyFailure(parseParcelWords(text)); message != "" {
		return fmt.Errorf("connectivity service rejected the request: %s", message)
	}
	return nil
}

// startTetheringViaBinder asks the connectivity service to start tethering.
func startTetheringViaBinder(tetherType int) error {
	codes, err := getTetheringBinderCodes()
	if err != nil {
		return err
	}

	args := fmt.Sprintf(" i32 %d i32 0 i32 0 s16 %s", tetherType, shellSingleQuote(tetheringCallerPkg))
	if err := callConnectivityTransaction(codes.Start, args); err != nil {
		return err
	}
	return nil
}

// stopTetheringViaBinder asks the connectivity service to stop tethering.
func stopTetheringViaBinder(tetherType int) error {
	codes, err := getTetheringBinderCodes()
	if err != nil {
		return err
	}

	args := fmt.Sprintf(" i32 %d s16 %s", tetherType, shellSingleQuote(tetheringCallerPkg))
	return callConnectivityTransaction(codes.Stop, args)
}

// loadTetheringCodesCache reads the cached transaction codes from disk.
func loadTetheringCodesCache() *tetheringBinderCodes {
	path := config.GetPersistentFilePath("tethering_codes.json")
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var codes tetheringBinderCodes
	if err := json.Unmarshal(buf, &codes); err != nil {
		return nil
	}
	if codes.Start <= 0 || codes.Stop <= codes.Start {
		return nil
	}
	return &codes
}

// saveTetheringCodesCache stores the discovered transaction codes on disk.
func saveTetheringCodesCache(codes tetheringBinderCodes) {
	buf, err := json.Marshal(codes)
	if err != nil {
		return
	}
	if err := config.WriteFileAtomic(config.GetPersistentFilePath("tethering_codes.json"), buf, 0644); err != nil {
		logger.Get().Warnf("hotspot", "unable to cache the tethering transactions: %v", err)
	}
}
