package store

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

const DefaultDesktopPort = 11211

type Store struct {
	dir string

	mu        sync.RWMutex
	cfg       config.Config
	stats     config.Stats
	db        *sql.DB
	chatMu    sync.Mutex
	cooldowns map[string]time.Time
}

func DefaultDir() string {
	if v := os.Getenv("FLASHROUTE_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".flashroute"
	}
	return filepath.Join(home, ".config", "flashroute")
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "uploads"), 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, cooldowns: map[string]time.Time{}}
	firstRun := false
	if _, err := os.Stat(s.configPath()); errors.Is(err, os.ErrNotExist) {
		firstRun = true
		s.cfg = config.Config{Version: 9}
		if err := s.saveConfigLocked(); err != nil {
			return nil, err
		}
	}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	if err := s.loadStats(); err != nil {
		return nil, err
	}
	if err := s.initChats(); err != nil {
		return nil, err
	}
	changed := false
	if s.cfg.Auth.SessionSecret == "" {
		s.cfg.Auth.SessionSecret = randomToken(32)
		changed = true
	}
	if s.cfg.LocalNoKey == nil {
		enabled := true
		s.cfg.LocalNoKey = &enabled
		changed = true
	}
	if s.cfg.RedactEnabled == nil {
		enabled := true
		s.cfg.RedactEnabled = &enabled
		changed = true
	}
	if s.cfg.RedactRules == nil {
		s.cfg.RedactRules = config.DefaultRedactRules()
		changed = true
	}
	if s.cfg.DesktopPort == 0 {
		s.cfg.DesktopPort = DefaultDesktopPort
		changed = true
	}
	for i := range s.cfg.RedactRules {
		switch {
		case s.cfg.RedactRules[i].ID == "api-key" && s.cfg.RedactRules[i].Pattern == config.LegacyAPIKeyRedactPattern:
			s.cfg.RedactRules[i].Pattern = config.APIKeyRedactPattern
			changed = true
		case s.cfg.RedactRules[i].ID == "bearer-token" && s.cfg.RedactRules[i].Pattern == config.LegacyBearerRedactPattern:
			s.cfg.RedactRules[i].Pattern = config.BearerRedactPattern
			changed = true
		case s.cfg.RedactRules[i].ID == "private-key" && s.cfg.RedactRules[i].Pattern == config.LegacyPrivateKeyRedactPattern:
			s.cfg.RedactRules[i].Pattern = config.PrivateKeyRedactPattern
			changed = true
		}
	}
	if s.cfg.Version < 2 {
		if _, ok := findRedactRule(s.cfg.RedactRules, "email"); !ok {
			for _, rule := range config.DefaultRedactRules() {
				if rule.ID == "email" {
					s.cfg.RedactRules = append(s.cfg.RedactRules, rule)
					break
				}
			}
		}
		s.cfg.Version = 2
		changed = true
	}
	if s.cfg.Version < 4 {
		for i := range s.cfg.RedactRules {
			if strings.TrimSpace(s.cfg.RedactRules[i].Group) != "" {
				continue
			}
			switch s.cfg.RedactRules[i].ID {
			case "api-key", "bearer-token", "private-key":
				s.cfg.RedactRules[i].Group = "keys"
			case "email":
				s.cfg.RedactRules[i].Group = "identity"
			}
			changed = true
		}
		s.cfg.Version = 4
		changed = true
	}
	if s.cfg.Version < 8 {
		for _, rule := range config.DefaultRedactRules() {
			if _, ok := findRedactRule(s.cfg.RedactRules, rule.ID); ok {
				continue
			}
			s.cfg.RedactRules = append(s.cfg.RedactRules, rule)
			changed = true
		}
		s.cfg.Version = 8
		changed = true
	}
	if s.cfg.Version < 9 {
		var pgpPattern string
		for _, rule := range config.DefaultRedactRules() {
			if rule.ID == "pgp-private-key" {
				pgpPattern = rule.Pattern
				break
			}
		}
		for i := range s.cfg.RedactRules {
			rule := &s.cfg.RedactRules[i]
			switch rule.ID {
			case "bearer-token":
				if rule.Pattern == config.LegacyBearerRedactPattern || rule.Pattern == config.LegacyBearerTokenPattern {
					rule.Pattern = config.BearerRedactPattern
					changed = true
				}
			case "email":
				if rule.Pattern == config.LegacyEmailRedactPattern || rule.Pattern == config.LegacyEmailLocalPattern {
					rule.Pattern = config.EmailRedactPattern
					changed = true
				}
			case "pgp-private-key":
				if pgpPattern != "" && rule.Pattern != pgpPattern {
					rule.Pattern = pgpPattern
					changed = true
				}
			}
		}
		s.cfg.Version = 9
		changed = true
	}
	if firstRun && len(s.cfg.APIKeys) == 0 {
		s.cfg.APIKeys = append(s.cfg.APIKeys, config.APIKey{
			ID:        NewID(),
			Name:      "Default",
			Key:       NewAPIKey(),
			CreatedAt: config.Now(),
		})
		changed = true
	}
	if changed {
		if err := s.saveConfigLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) UploadDir() string { return filepath.Join(s.dir, "uploads") }

func (s *Store) configPath() string { return filepath.Join(s.dir, "config.yaml") }
func (s *Store) statsPath() string  { return filepath.Join(s.dir, "stats.yaml") }

func (s *Store) loadConfig() error {
	b, err := os.ReadFile(s.configPath())
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, &s.cfg); err != nil {
		return fmt.Errorf("parse %s: %w", s.configPath(), err)
	}
	if s.cfg.Version == 0 {
		s.cfg.Version = 1
	}
	return nil
}

func (s *Store) loadStats() error {
	b, err := os.ReadFile(s.statsPath())
	if errors.Is(err, os.ErrNotExist) {
		s.stats = config.Stats{Routes: map[string]*config.RouteStats{}}
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, &s.stats); err != nil {
		return fmt.Errorf("parse %s: %w", s.statsPath(), err)
	}
	if s.stats.Routes == nil {
		s.stats.Routes = map[string]*config.RouteStats{}
	}
	return nil
}

func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".flashroute-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *Store) saveConfigLocked() error { return writeYAML(s.configPath(), &s.cfg) }
func (s *Store) saveStatsLocked() error  { return writeYAML(s.statsPath(), &s.stats) }

func (s *Store) Snapshot() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func cloneConfig(c config.Config) config.Config {
	out := c
	out.Providers = make([]config.Provider, len(c.Providers))
	for i, p := range c.Providers {
		np := p
		np.Models = make([]config.Model, len(p.Models))
		for j, m := range p.Models {
			nm := m
			if m.LastTest != nil {
				t := *m.LastTest
				nm.LastTest = &t
			}
			np.Models[j] = nm
		}
		out.Providers[i] = np
	}
	out.Routes = make([]config.Route, len(c.Routes))
	for i, r := range c.Routes {
		nr := r
		nr.Targets = append([]config.RouteTarget(nil), r.Targets...)
		out.Routes[i] = nr
	}
	out.APIKeys = append([]config.APIKey(nil), c.APIKeys...)
	if c.RedactEnabled != nil {
		enabled := *c.RedactEnabled
		out.RedactEnabled = &enabled
	}
	out.RedactRules = append([]config.RedactRule(nil), c.RedactRules...)
	return out
}

func (s *Store) Provider(id string) (config.Provider, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.cfg.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return config.Provider{}, false
}

func (s *Store) FindProviderByModel(model string) (config.Provider, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.cfg.Providers {
		if strings.HasPrefix(model, p.ID+"/") {
			return p, strings.TrimPrefix(model, p.ID+"/"), true
		}
	}
	return config.Provider{}, "", false
}

func (s *Store) FindRoute(alias string) (config.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.cfg.Routes {
		if r.Alias == alias {
			return r, true
		}
	}
	return config.Route{}, false
}

func (s *Store) HasRoute(alias string) bool {
	_, ok := s.FindRoute(alias)
	return ok
}

func (s *Store) UpsertProvider(p config.Provider) (config.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := config.Now()
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if p.BaseURL == "" {
		return p, errors.New("base_url is required")
	}
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	for _, existing := range s.cfg.Providers {
		if existing.Name == p.Name && existing.ID != p.ID {
			return p, fmt.Errorf("provider name %q %w", p.Name, ErrConflict)
		}
	}
	for i := range s.cfg.Providers {
		if s.cfg.Providers[i].ID != p.ID {
			continue
		}
		p.CreatedAt = s.cfg.Providers[i].CreatedAt
		if p.Models == nil {
			p.Models = s.cfg.Providers[i].Models
		}
		s.cfg.Providers[i] = p
		if err := s.saveConfigLocked(); err != nil {
			return p, err
		}
		return p, nil
	}
	s.cfg.Providers = append(s.cfg.Providers, p)
	if err := s.saveConfigLocked(); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Store) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.cfg.Routes {
		for _, t := range r.Targets {
			if t.ProviderID == id {
				return fmt.Errorf("provider is used by route %q", r.Alias)
			}
		}
	}
	idx := -1
	for i := range s.cfg.Providers {
		if s.cfg.Providers[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	s.cfg.Providers = append(s.cfg.Providers[:idx], s.cfg.Providers[idx+1:]...)
	return s.saveConfigLocked()
}

func (s *Store) SetProviderModels(providerID string, models []config.Model) ([]config.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Providers {
		if s.cfg.Providers[i].ID != providerID {
			continue
		}
		prev := map[string]config.Model{}
		for _, m := range s.cfg.Providers[i].Models {
			prev[m.ID] = m
		}
		now := config.Now()
		for j := range models {
			if old, ok := prev[models[j].ID]; ok {
				models[j].LastTest = old.LastTest
			}
			models[j].LastFetchedAt = now
		}
		s.cfg.Providers[i].Models = models
		s.cfg.Providers[i].UpdatedAt = now
		if err := s.saveConfigLocked(); err != nil {
			return nil, err
		}
		return cloneModels(models), nil
	}
	return nil, ErrNotFound
}

func cloneModels(models []config.Model) []config.Model {
	out := make([]config.Model, len(models))
	for i, m := range models {
		out[i] = m
		if m.LastTest != nil {
			t := *m.LastTest
			out[i].LastTest = &t
		}
	}
	return out
}

func (s *Store) SetModelTest(providerID, modelID string, test config.ModelTest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Providers {
		if s.cfg.Providers[i].ID != providerID {
			continue
		}
		for j := range s.cfg.Providers[i].Models {
			if s.cfg.Providers[i].Models[j].ID == modelID {
				s.cfg.Providers[i].Models[j].LastTest = &test
				return s.saveConfigLocked()
			}
		}
		return ErrNotFound
	}
	return ErrNotFound
}

func (s *Store) UpsertRoute(r config.Route) (config.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := config.Now()
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.Alias == "" {
		return r, errors.New("alias is required")
	}
	if len(r.Targets) == 0 {
		return r, errors.New("at least one target is required")
	}
	for _, t := range r.Targets {
		found := false
		for _, p := range s.cfg.Providers {
			if p.ID == t.ProviderID {
				found = true
				break
			}
		}
		if !found {
			return r, fmt.Errorf("provider %q not found", t.ProviderID)
		}
	}
	for _, existing := range s.cfg.Routes {
		if existing.Alias == r.Alias && existing.ID != r.ID {
			return r, fmt.Errorf("alias %q %w", r.Alias, ErrConflict)
		}
	}
	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	for i := range s.cfg.Routes {
		if s.cfg.Routes[i].ID == r.ID {
			r.CreatedAt = s.cfg.Routes[i].CreatedAt
			s.cfg.Routes[i] = r
			return r, s.saveConfigLocked()
		}
	}
	for _, existing := range s.cfg.Routes {
		if existing.Alias == r.Alias {
			return r, fmt.Errorf("alias %q %w", r.Alias, ErrConflict)
		}
	}
	s.cfg.Routes = append(s.cfg.Routes, r)
	return r, s.saveConfigLocked()
}

func (s *Store) DeleteRoute(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Routes {
		if s.cfg.Routes[i].ID == id {
			s.cfg.Routes = append(s.cfg.Routes[:i], s.cfg.Routes[i+1:]...)
			return s.saveConfigLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) AddAPIKey(name string) (config.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(name) == "" {
		name = "API Key"
	}
	k := config.APIKey{ID: NewID(), Name: name, Key: NewAPIKey(), CreatedAt: config.Now()}
	s.cfg.APIKeys = append(s.cfg.APIKeys, k)
	return k, s.saveConfigLocked()
}

func (s *Store) DeleteAPIKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.APIKeys {
		if s.cfg.APIKeys[i].ID == id {
			s.cfg.APIKeys = append(s.cfg.APIKeys[:i], s.cfg.APIKeys[i+1:]...)
			return s.saveConfigLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) RenameAPIKey(id, name string) (config.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.APIKeys {
		if s.cfg.APIKeys[i].ID == id {
			if strings.TrimSpace(name) != "" {
				s.cfg.APIKeys[i].Name = strings.TrimSpace(name)
			}
			return s.cfg.APIKeys[i], s.saveConfigLocked()
		}
	}
	return config.APIKey{}, ErrNotFound
}

func (s *Store) ValidateAPIKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(k.Key), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

func (s *Store) RedactRules() []config.RedactRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]config.RedactRule(nil), s.cfg.RedactRules...)
}

func (s *Store) LocalNoKeyEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.LocalNoKey == nil || *s.cfg.LocalNoKey
}

func (s *Store) SetLocalNoKey(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.LocalNoKey = &enabled
	return s.saveConfigLocked()
}

func (s *Store) RedactionEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.RedactEnabled == nil || *s.cfg.RedactEnabled
}

func (s *Store) SetRedactionEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.RedactEnabled = &enabled
	return s.saveConfigLocked()
}

func (s *Store) SetRedactRules(rules []config.RedactRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]config.RedactRule, len(rules))
	for i, r := range rules {
		if strings.TrimSpace(r.ID) == "" {
			r.ID = NewID()
		}
		out[i] = r
	}
	s.cfg.RedactRules = out
	return s.saveConfigLocked()
}

func (s *Store) PasswordHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Auth.PasswordHash
}

func (s *Store) SetPasswordHash(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Auth.PasswordHash = hash
	return s.saveConfigLocked()
}

func (s *Store) DesktopPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.DesktopPort == 0 {
		return DefaultDesktopPort
	}
	return s.cfg.DesktopPort
}

func (s *Store) SetDesktopPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.DesktopPort = port
	return s.saveConfigLocked()
}

func (s *Store) SessionSecret() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return []byte(s.cfg.Auth.SessionSecret)
}

func targetCooldownKey(provider, model string) string {
	return provider + "::" + model
}

func (s *Store) SetTargetCooldown(provider, model string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cooldowns == nil {
		s.cooldowns = map[string]time.Time{}
	}
	s.cooldowns[targetCooldownKey(provider, model)] = until
}

func (s *Store) TargetCooldown(provider, model string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cooldowns[targetCooldownKey(provider, model)]
}

func (s *Store) ClearTargetCooldown(provider, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cooldowns, targetCooldownKey(provider, model))
}

func (s *Store) RecordRoute(alias string, success bool, attempts []Attempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats.Routes == nil {
		s.stats.Routes = map[string]*config.RouteStats{}
	}
	rs := s.stats.Routes[alias]
	if rs == nil {
		rs = &config.RouteStats{Targets: map[string]*config.TargetStats{}}
		s.stats.Routes[alias] = rs
	}
	if rs.Targets == nil {
		rs.Targets = map[string]*config.TargetStats{}
	}
	rs.Requests++
	rs.UpdatedAt = config.Now()
	if success {
		rs.Success++
	} else {
		rs.Failure++
	}
	for _, a := range attempts {
		key := a.Provider + "::" + a.Model
		ts := rs.Targets[key]
		if ts == nil {
			ts = &config.TargetStats{Provider: a.Provider, Model: a.Model}
			rs.Targets[key] = ts
		}
		ts.Attempts++
		switch a.Outcome {
		case "success":
			ts.Success++
		case "rate_limit":
			ts.RateLimit++
			ts.Errors++
		default:
			ts.Errors++
		}
	}
	_ = s.saveStatsLocked()
}

func (s *Store) StatsSnapshot(aliases []string) map[string]config.RouteStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]config.RouteStats{}
	now := time.Now()
	for _, alias := range aliases {
		if rs := s.stats.Routes[alias]; rs != nil {
			cloned := cloneRouteStats(rs)
			for _, ts := range cloned.Targets {
				if until, ok := s.cooldowns[targetCooldownKey(ts.Provider, ts.Model)]; ok && until.After(now) {
					ts.CooldownUntil = until.Format(time.RFC3339)
				}
			}
			out[alias] = *cloned
		}
	}
	return out
}

func cloneRouteStats(rs *config.RouteStats) *config.RouteStats {
	out := *rs
	out.Targets = map[string]*config.TargetStats{}
	for k, t := range rs.Targets {
		nt := *t
		out.Targets[k] = &nt
	}
	return &out
}

type Attempt struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Outcome  string `json:"outcome"`
}

func NewID() string {
	return randomToken(12)
}

func NewAPIKey() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 40)
	raw := make([]byte, 40)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return "sk-fr-" + string(b)
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func findRedactRule(rules []config.RedactRule, id string) (config.RedactRule, bool) {
	for _, rule := range rules {
		if rule.ID == id {
			return rule, true
		}
	}
	return config.RedactRule{}, false
}
