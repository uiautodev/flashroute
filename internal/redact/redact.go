package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uiautodev/flashroute/internal/config"
)

const (
	maskAlphabet   = "abcdefghijklmnopqrstuvwxyz0123456789"
	upperAlphabet  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digitsAlphabet = "0123456789"
	defaultSecret  = "flashroute-redaction-secret"
)

type rule struct {
	id          string
	name        string
	replacement string
	re          *regexp.Regexp
}

type Hit struct {
	RuleID      string
	RuleName    string
	Original    string
	Replacement string
}

type Redactor struct {
	rules      []rule
	secret     []byte
	record     bool
	hits       []Hit
	byOriginal map[string]string
	reverse    map[string]string
	maxTagLen  int
}

func Compile(rules []config.RedactRule) *Redactor {
	r, _ := compile(rules, false)
	return r
}

func CompileWithSecret(rules []config.RedactRule, secret []byte) *Redactor {
	r, _ := compile(rules, false)
	if len(secret) > 0 {
		r.secret = secret
	}
	return r
}

func CompileStrict(rules []config.RedactRule) (*Redactor, error) {
	return compile(rules, true)
}

func compile(rules []config.RedactRule, strict bool) (*Redactor, error) {
	out := &Redactor{
		rules:      make([]rule, 0, len(rules)),
		secret:     []byte(defaultSecret),
		byOriginal: map[string]string{},
		reverse:    map[string]string{},
	}
	for _, item := range rules {
		if !item.Enabled || strings.TrimSpace(item.Pattern) == "" {
			continue
		}
		re, err := regexp.Compile(item.Pattern)
		if err != nil {
			if strict {
				return nil, fmt.Errorf("invalid pattern %q: %w", item.Pattern, err)
			}
			continue
		}
		out.rules = append(out.rules, rule{
			id: item.ID, name: item.Name,
			replacement: strings.TrimSpace(item.Replacement), re: re,
		})
	}
	return out, nil
}

func (r *Redactor) StartRecording() {
	r.record = true
}

func (r *Redactor) Hits() []Hit {
	return append([]Hit(nil), r.hits...)
}

func (r *Redactor) MaskText(value string) string {
	for _, item := range r.rules {
		value = r.maskRule(item, value)
	}
	return value
}

func (r *Redactor) maskRule(item rule, value string) string {
	matches := item.re.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		out.WriteString(value[last:match[0]])
		original := value[match[0]:match[1]]
		var replacement string
		if item.replacement != "" {
			replacement = r.maskReplacement(item, value, match)
		} else {
			replacement = r.maskSecret(item, original)
		}
		if r.record {
			r.hits = append(r.hits, Hit{
				RuleID: item.id, RuleName: item.name,
				Original: original, Replacement: replacement,
			})
		}
		out.WriteString(replacement)
		last = match[1]
	}
	out.WriteString(value[last:])
	return out.String()
}

func (r *Redactor) maskSecret(item rule, secret string) string {
	if secret == "" {
		return ""
	}
	if tag, ok := r.byOriginal[secret]; ok {
		return tag
	}
	for attempt := 0; attempt < 64; attempt++ {
		tag := r.formatMask(item, secret, attempt)
		if tag == secret {
			continue
		}
		if _, used := r.reverse[tag]; !used {
			r.storeMapping(secret, tag)
			return tag
		}
	}
	tag := fmt.Sprintf("sk-%x", sha256.Sum256(append(append([]byte{}, r.secret...), secret...)))
	r.storeMapping(secret, tag)
	return tag
}

func (r *Redactor) maskReplacement(item rule, source string, match []int) string {
	original := source[match[0]:match[1]]
	if tag, ok := r.byOriginal[original]; ok {
		return tag
	}
	for attempt := 0; attempt < 64; attempt++ {
		tag := r.buildReplacement(item, original, source, match, attempt)
		if tag == original {
			continue
		}
		if _, used := r.reverse[tag]; !used {
			r.storeMapping(original, tag)
			return tag
		}
	}
	tag := r.fallbackReplacement(item, original)
	r.storeMapping(original, tag)
	return tag
}

func (r *Redactor) fallbackReplacement(item rule, original string) string {
	for attempt := 0; attempt < 64; attempt++ {
		tag := r.maskChars(item.id, original, original, len(original), attempt)
		if tag != original {
			if _, used := r.reverse[tag]; !used {
				return tag
			}
		}
	}
	return fmt.Sprintf("sk-%x", sha256.Sum256(append(append([]byte{}, r.secret...), original...)))
}

func expandBackrefs(template, source string, match []int) string {
	var out strings.Builder
	for i := 0; i < len(template); i++ {
		c := template[i]
		if c != '$' || i+1 >= len(template) {
			out.WriteByte(c)
			continue
		}
		next := template[i+1]
		switch {
		case next == '$':
			out.WriteByte('$')
			i++
		case next == '&':
			out.WriteString(source[match[0]:match[1]])
			i++
		case next == '{':
			end := strings.IndexByte(template[i+2:], '}')
			if end < 0 {
				out.WriteByte(c)
				continue
			}
			group := template[i+2 : i+2+end]
			if n, ok := parseGroupIndex(group); ok {
				out.WriteString(submatch(source, match, n))
			}
			i += 2 + end
		case next >= '0' && next <= '9':
			j := i + 1
			for j < len(template) && template[j] >= '0' && template[j] <= '9' {
				j++
			}
			if n, ok := parseGroupIndex(template[i+1 : j]); ok {
				out.WriteString(submatch(source, match, n))
			}
			i = j - 1
		default:
			out.WriteByte('$')
		}
	}
	return out.String()
}

func parseGroupIndex(value string) (int, bool) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func submatch(source string, match []int, n int) string {
	if n < 0 || 2*n+1 >= len(match) || match[2*n] < 0 {
		return ""
	}
	return source[match[2*n]:match[2*n+1]]
}

var starRunPattern = regexp.MustCompile(`\*+`)

func (r *Redactor) buildReplacement(item rule, original, source string, match []int, attempt int) string {
	replacement := expandBackrefs(item.replacement, source, match)
	loc := starRunPattern.FindStringIndex(replacement)
	if loc == nil {
		return replacement
	}
	before := replacement[:loc[0]]
	after := starRunPattern.ReplaceAllString(replacement[loc[1]:], "")
	generatedLen := len(original) - len(before) - len(after)
	if generatedLen < 0 {
		generatedLen = 0
	}
	shapeStart := len(before)
	if shapeStart > len(original) {
		shapeStart = len(original)
	}
	shapeEnd := len(original) - len(after)
	if shapeEnd < shapeStart {
		shapeEnd = shapeStart
	}
	shape := original[shapeStart:shapeEnd]
	return before + r.maskChars(item.id, original, shape, generatedLen, attempt) + after
}

func (r *Redactor) maskChars(ruleID, original, shape string, length, attempt int) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(ruleID))
	mac.Write([]byte{0})
	mac.Write([]byte(original))
	var attemptBytes [8]byte
	binary.BigEndian.PutUint64(attemptBytes[:], uint64(attempt))
	mac.Write(attemptBytes[:])
	sum := mac.Sum(nil)
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		c := byte(0)
		if i < len(shape) {
			c = shape[i]
		}
		switch {
		case c >= '0' && c <= '9':
			out[i] = digitsAlphabet[int(sum[i%len(sum)])%len(digitsAlphabet)]
		case c >= 'A' && c <= 'Z':
			out[i] = upperAlphabet[int(sum[i%len(sum)])%len(upperAlphabet)]
		case c >= 'a' && c <= 'z':
			out[i] = maskAlphabet[int(sum[i%len(sum)])%len(maskAlphabet)]
		case c != 0 && !isAlphaNumeric(c):
			out[i] = c
		default:
			out[i] = maskAlphabet[int(sum[i%len(sum)])%len(maskAlphabet)]
		}
	}
	return string(out)
}

func (r *Redactor) storeMapping(original, tag string) {
	r.byOriginal[original] = tag
	r.reverse[tag] = original
	if len(tag) > r.maxTagLen {
		r.maxTagLen = len(tag)
	}
}

func (r *Redactor) formatMask(item rule, secret string, attempt int) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(item.id))
	mac.Write([]byte{0})
	mac.Write([]byte(secret))
	var attemptBytes [8]byte
	binary.BigEndian.PutUint64(attemptBytes[:], uint64(attempt))
	mac.Write(attemptBytes[:])
	sum := mac.Sum(nil)
	masked := make([]byte, len(secret))
	for i := 0; i < len(secret); i++ {
		c := secret[i]
		switch {
		case c >= 'A' && c <= 'Z':
			masked[i] = upperAlphabet[int(sum[i%len(sum)])%len(upperAlphabet)]
		case c >= '0' && c <= '9':
			masked[i] = digitsAlphabet[int(sum[i%len(sum)])%len(digitsAlphabet)]
		case isAlphaNumeric(c):
			masked[i] = maskAlphabet[int(sum[i%len(sum)])%len(maskAlphabet)]
		default:
			masked[i] = c
		}
	}
	return string(masked)
}

func isAlphaNumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (r *Redactor) MaskValue(value any) any {
	switch v := value.(type) {
	case string:
		return r.MaskText(v)
	case []any:
		for i := range v {
			v[i] = r.MaskValue(v[i])
		}
		return v
	case map[string]any:
		for key, item := range v {
			v[key] = r.MaskValue(item)
		}
		return v
	default:
		return value
	}
}

func (r *Redactor) Restore(value string) string {
	if len(r.reverse) == 0 {
		return value
	}
	tags := make([]string, 0, len(r.reverse))
	for tag := range r.reverse {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool { return len(tags[i]) > len(tags[j]) })
	pairs := make([]string, 0, len(tags)*2)
	for _, tag := range tags {
		pairs = append(pairs, tag, r.reverse[tag])
	}
	replacer := strings.NewReplacer(pairs...)
	for i := 0; i < 8; i++ {
		next := replacer.Replace(value)
		if next == value {
			return value
		}
		value = next
	}
	return value
}

func (r *Redactor) RestoreValue(value any) any {
	switch v := value.(type) {
	case string:
		return r.Restore(v)
	case []any:
		for i := range v {
			v[i] = r.RestoreValue(v[i])
		}
		return v
	case map[string]any:
		for key, item := range v {
			v[key] = r.RestoreValue(item)
		}
		return v
	default:
		return value
	}
}

func (r *Redactor) RestoreJSON(body []byte) ([]byte, bool) {
	if !r.HasMappings() {
		return body, true
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	value = r.RestoreValue(value)
	restored, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return restored, true
}

func (r *Redactor) HasMappings() bool {
	return len(r.reverse) > 0
}

func (r *Redactor) Messages(obj map[string]json.RawMessage) {
	raw, ok := obj["messages"]
	if !ok {
		return
	}
	var messages any
	if err := json.Unmarshal(raw, &messages); err != nil {
		return
	}
	body, err := json.Marshal(r.MaskValue(messages))
	if err != nil {
		return
	}
	obj["messages"] = body
}

func (r *Redactor) RestoreReader(src io.Reader) io.Reader {
	if !r.HasMappings() {
		return src
	}
	return &restoreReader{src: src, redactor: r, maxLen: r.maxTagLen}
}

type StreamRestorer struct {
	redactor *Redactor
	pending  string
}

func (r *Redactor) NewStreamRestorer() *StreamRestorer {
	return &StreamRestorer{redactor: r}
}

func (s *StreamRestorer) Push(delta string) string {
	s.pending += delta
	cut := s.redactor.safeCut(s.pending)
	out := s.redactor.Restore(s.pending[:cut])
	s.pending = s.pending[cut:]
	return out
}

func (s *StreamRestorer) Flush() string {
	out := s.redactor.Restore(s.pending)
	s.pending = ""
	return out
}

func (r *Redactor) safeCut(value string) int {
	start := len(value) - r.maxTagLen
	if start < 0 {
		start = 0
	}
	for i := start; i < len(value); i++ {
		if r.couldBeTagPrefix(value[i:]) {
			return i
		}
	}
	return len(value)
}

func (r *Redactor) couldBeTagPrefix(value string) bool {
	for tag := range r.reverse {
		if len(value) < len(tag) && strings.HasPrefix(tag, value) {
			return true
		}
	}
	return false
}

type restoreReader struct {
	src      io.Reader
	redactor *Redactor
	maxLen   int
	pending  []byte
	out      []byte
	eof      bool
}

func (r *restoreReader) Read(p []byte) (int, error) {
	for {
		if len(r.out) > 0 {
			n := copy(p, r.out)
			r.out = r.out[n:]
			return n, nil
		}
		if r.eof {
			return 0, io.EOF
		}
		buf := make([]byte, 32<<10)
		n, err := r.src.Read(buf)
		if n > 0 {
			r.pending = append(r.pending, buf[:n]...)
			cut := r.redactor.safeCut(string(r.pending))
			if cut > 0 {
				r.out = append(r.out, r.redactor.Restore(string(r.pending[:cut]))...)
				r.pending = r.pending[cut:]
			}
		}
		if err != nil {
			r.eof = true
			if len(r.pending) > 0 {
				r.out = append(r.out, r.redactor.Restore(string(r.pending))...)
				r.pending = nil
			}
			if len(r.out) == 0 {
				return 0, err
			}
			n := copy(p, r.out)
			r.out = r.out[n:]
			return n, nil
		}
		if len(r.out) > 0 {
			n := copy(p, r.out)
			r.out = r.out[n:]
			return n, nil
		}
	}
}
