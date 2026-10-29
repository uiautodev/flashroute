package redact

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/uiautodev/flashroute/internal/config"
)

type rule struct {
	re *regexp.Regexp
}

type Redactor struct {
	rules      []rule
	nonce      string
	next       int
	byOriginal map[string]string
	reverse    map[string]string
	maxTagLen  int
}

func Compile(rules []config.RedactRule) *Redactor {
	r, _ := compile(rules, false)
	return r
}

func CompileStrict(rules []config.RedactRule) (*Redactor, error) {
	return compile(rules, true)
}

func compile(rules []config.RedactRule, strict bool) (*Redactor, error) {
	out := &Redactor{
		rules:      make([]rule, 0, len(rules)),
		nonce:      newNonce(),
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
		out.rules = append(out.rules, rule{re: re})
	}
	return out, nil
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
		start, end := match[0], match[1]
		if len(match) >= 4 && match[2] == match[0] && match[3] <= match[1] {
			out.WriteString(value[match[2]:match[3]])
			start = match[3]
		}
		out.WriteString(r.maskSecret(value[start:end]))
		last = match[1]
	}
	out.WriteString(value[last:])
	return out.String()
}

func (r *Redactor) maskSecret(secret string) string {
	if tag, ok := r.byOriginal[secret]; ok {
		return tag
	}
	r.next++
	tag := fmt.Sprintf("sk-%s-%d", r.nonce, r.next)
	r.byOriginal[secret] = tag
	r.reverse[tag] = secret
	if len(tag) > r.maxTagLen {
		r.maxTagLen = len(tag)
	}
	return tag
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
	for tag, original := range r.reverse {
		value = strings.ReplaceAll(value, tag, original)
	}
	return value
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

func newNonce() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "000000000000"
	}
	return hex.EncodeToString(buf)
}
