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
	id   string
	name string
	re   *regexp.Regexp
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
		out.rules = append(out.rules, rule{id: item.ID, name: item.Name, re: re})
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
		replacement := r.maskMatch(item, value, match)
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

func (r *Redactor) maskMatch(item rule, source string, match []int) string {
	start, end := match[0], match[1]
	type span struct{ start, end int }
	spans := make([]span, 0, (len(match)-2)/2)
	for i := 1; i*2+1 < len(match); i++ {
		groupStart, groupEnd := match[i*2], match[i*2+1]
		if groupStart < 0 || groupEnd < groupStart {
			continue
		}
		spans = append(spans, span{groupStart, groupEnd})
	}
	if len(spans) == 0 {
		return r.maskSecret(item, source[start:end])
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := spans[:0]
	for _, current := range spans {
		if len(merged) == 0 || current.start > merged[len(merged)-1].end {
			merged = append(merged, current)
			continue
		}
		if current.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = current.end
		}
	}
	var out strings.Builder
	cursor := start
	for _, keep := range merged {
		if keep.start > cursor {
			out.WriteString(r.maskSecret(item, source[cursor:keep.start]))
		}
		out.WriteString(source[keep.start:keep.end])
		cursor = keep.end
	}
	if cursor < end {
		out.WriteString(r.maskSecret(item, source[cursor:end]))
	}
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
