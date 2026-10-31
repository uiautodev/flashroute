package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/redact"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const chatSchema = `
CREATE TABLE IF NOT EXISTS chats (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	active_message_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	chat_id TEXT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	reasoning TEXT NOT NULL DEFAULT '',
	images TEXT NOT NULL DEFAULT '[]',
	model TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	redacted INTEGER NOT NULL DEFAULT 0,
	tools TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(chat_id);
CREATE INDEX IF NOT EXISTS idx_messages_parent ON messages(chat_id, parent_id);
CREATE TABLE IF NOT EXISTS redactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	rule_id TEXT NOT NULL,
	rule_name TEXT NOT NULL DEFAULT '',
	original TEXT NOT NULL,
	replacement TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_redactions_rule ON redactions(rule_id, created_at);
CREATE INDEX IF NOT EXISTS idx_redactions_created ON redactions(created_at);
`

func (s *Store) initChats() error {
	if err := s.migrateChatDBName(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filepath.Join(s.dir, "flashroute.db"))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	s.db = db
	if _, err := db.Exec(chatSchema); err != nil {
		return fmt.Errorf("init chat database: %w", err)
	}
	if err := ensureMessageColumn(db, "redacted", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("migrate chat database: %w", err)
	}
	if err := ensureMessageColumn(db, "tools", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return fmt.Errorf("migrate chat database: %w", err)
	}
	if err := s.migrateLegacyChats(); err != nil {
		return err
	}
	return s.pruneRedactions()
}

func (s *Store) migrateChatDBName() error {
	newPath := filepath.Join(s.dir, "flashroute.db")
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	oldPath := filepath.Join(s.dir, "chats.db")
	if _, err := os.Stat(oldPath); err != nil {
		return nil
	}
	return os.Rename(oldPath, newPath)
}

func ensureMessageColumn(db *sql.DB, name, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE messages ADD COLUMN %s %s", name, definition))
	return err
}

func (s *Store) loadAllMessages(chatID string) ([]config.ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, role, content, reasoning, images, model, provider, redacted, tools, created_at
		FROM messages WHERE chat_id = ? ORDER BY created_at, rowid`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.ChatMessage
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	return out, rows.Err()
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row messageScanner) (config.ChatMessage, error) {
	var message config.ChatMessage
	var images, tools string
	if err := row.Scan(&message.ID, &message.ParentID, &message.Role, &message.Content, &message.Reasoning,
		&images, &message.Model, &message.Provider, &message.Redacted, &tools, &message.CreatedAt); err != nil {
		return message, err
	}
	_ = json.Unmarshal([]byte(images), &message.Images)
	_ = json.Unmarshal([]byte(tools), &message.Tools)
	return message, nil
}

func (s *Store) loadChatPath(chatID, activeID string) ([]config.ChatMessage, error) {
	all, err := s.loadAllMessages(chatID)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []config.ChatMessage{}, nil
	}
	byID := make(map[string]config.ChatMessage, len(all))
	index := make(map[string]int, len(all))
	groups := map[string][]string{}
	for i, message := range all {
		byID[message.ID] = message
		index[message.ID] = i
		key := message.ParentID + "\x00" + message.Role
		groups[key] = append(groups[key], message.ID)
	}
	if activeID == "" {
		activeID = all[len(all)-1].ID
	}
	var reversed []config.ChatMessage
	seen := map[string]bool{}
	for current := activeID; current != "" && !seen[current]; {
		message, ok := byID[current]
		if !ok {
			break
		}
		seen[current] = true
		reversed = append(reversed, message)
		current = message.ParentID
	}
	path := make([]config.ChatMessage, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		message := reversed[i]
		key := message.ParentID + "\x00" + message.Role
		siblings := groups[key]
		message.VersionCount = len(siblings)
		for j, id := range siblings {
			if id == message.ID {
				message.VersionIndex = j + 1
				break
			}
		}
		path = append(path, message)
	}
	return path, nil
}

func (s *Store) chatRow(chatID string) (config.Chat, bool, error) {
	var chat config.Chat
	err := s.db.QueryRow(`SELECT id, title, model, active_message_id, created_at, updated_at FROM chats WHERE id = ?`, chatID).
		Scan(&chat.ID, &chat.Title, &chat.Model, &chat.ActiveMessageID, &chat.CreatedAt, &chat.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return chat, false, nil
	}
	if err != nil {
		return chat, false, err
	}
	return chat, true, nil
}

func (s *Store) ListChats() []config.Chat {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	rows, err := s.db.Query(`SELECT id, title, model, active_message_id, created_at, updated_at
		FROM chats ORDER BY updated_at DESC`)
	if err != nil {
		return nil
	}
	var chats []config.Chat
	for rows.Next() {
		var chat config.Chat
		if err := rows.Scan(&chat.ID, &chat.Title, &chat.Model, &chat.ActiveMessageID, &chat.CreatedAt, &chat.UpdatedAt); err != nil {
			continue
		}
		chats = append(chats, chat)
	}
	rows.Close()
	for i := range chats {
		chats[i].Messages, _ = s.loadChatPath(chats[i].ID, chats[i].ActiveMessageID)
	}
	return chats
}

func (s *Store) GetChat(id string) (config.Chat, bool) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chat, ok, err := s.chatRow(id)
	if err != nil || !ok {
		return config.Chat{}, false
	}
	chat.Messages, err = s.loadChatPath(id, chat.ActiveMessageID)
	if err != nil {
		return config.Chat{}, false
	}
	return chat, true
}

func (s *Store) CreateChat(chat config.Chat) error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	_, err := s.db.Exec(`INSERT INTO chats (id, title, model, active_message_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		chat.ID, chat.Title, chat.Model, chat.ActiveMessageID, chat.CreatedAt, chat.UpdatedAt)
	return err
}

func (s *Store) InsertMessage(chatID string, message config.ChatMessage) error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	images, _ := json.Marshal(message.Images)
	tools, _ := json.Marshal(message.Tools)
	_, err := s.db.Exec(`INSERT INTO messages
		(id, chat_id, parent_id, role, content, reasoning, images, model, provider, redacted, tools, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, chatID, message.ParentID, message.Role, message.Content, message.Reasoning,
		string(images), message.Model, message.Provider, message.Redacted, string(tools), message.CreatedAt)
	return err
}

func (s *Store) SetActiveMessage(chatID, messageID, updatedAt string) error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if updatedAt == "" {
		_, err := s.db.Exec(`UPDATE chats SET active_message_id = ? WHERE id = ?`, messageID, chatID)
		return err
	}
	_, err := s.db.Exec(`UPDATE chats SET active_message_id = ?, updated_at = ? WHERE id = ?`, messageID, updatedAt, chatID)
	return err
}

func (s *Store) UpdateChatMeta(chatID, title, model, updatedAt string) error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	_, err := s.db.Exec(`UPDATE chats SET title = ?, model = ?, updated_at = ? WHERE id = ?`, title, model, updatedAt, chatID)
	return err
}

func (s *Store) Message(chatID, messageID string) (config.ChatMessage, bool) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.messageLocked(chatID, messageID)
}

func (s *Store) messageLocked(chatID, messageID string) (config.ChatMessage, bool) {
	row := s.db.QueryRow(`SELECT id, parent_id, role, content, reasoning, images, model, provider, redacted, tools, created_at
		FROM messages WHERE chat_id = ? AND id = ?`, chatID, messageID)
	message, err := scanMessage(row)
	if err != nil {
		return config.ChatMessage{}, false
	}
	return message, true
}

func (s *Store) siblingsLocked(chatID, parentID, role string) []config.ChatMessage {
	rows, err := s.db.Query(`SELECT id, parent_id, role, content, reasoning, images, model, provider, redacted, tools, created_at
		FROM messages WHERE chat_id = ? AND parent_id = ? AND role = ?
		ORDER BY created_at, rowid`, chatID, parentID, role)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []config.ChatMessage
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil
		}
		out = append(out, message)
	}
	return out
}

func (s *Store) latestLeafLocked(chatID, messageID string) string {
	current := messageID
	for i := 0; i < 1000; i++ {
		var child string
		err := s.db.QueryRow(`SELECT id FROM messages WHERE chat_id = ? AND parent_id = ?
			ORDER BY created_at DESC, rowid DESC LIMIT 1`, chatID, current).Scan(&child)
		if err != nil {
			break
		}
		current = child
	}
	return current
}

func (s *Store) PathTo(chatID, messageID string) ([]config.ChatMessage, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.loadChatPath(chatID, messageID)
}

func (s *Store) ActivateMessageVersion(chatID, messageID string, index int) (config.Chat, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	message, ok := s.messageLocked(chatID, messageID)
	if !ok {
		return config.Chat{}, ErrNotFound
	}
	siblings := s.siblingsLocked(chatID, message.ParentID, message.Role)
	if index < 1 || index > len(siblings) {
		return config.Chat{}, ErrNotFound
	}
	leaf := s.latestLeafLocked(chatID, siblings[index-1].ID)
	if _, err := s.db.Exec(`UPDATE chats SET active_message_id = ? WHERE id = ?`, leaf, chatID); err != nil {
		return config.Chat{}, err
	}
	chat, ok, err := s.chatRow(chatID)
	if err != nil || !ok {
		return config.Chat{}, ErrNotFound
	}
	chat.Messages, err = s.loadChatPath(chatID, leaf)
	return chat, err
}

func (s *Store) DeleteChat(id string) ([]string, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	rows, err := s.db.Query(`SELECT images FROM messages WHERE chat_id = ?`, id)
	if err != nil {
		return nil, err
	}
	var images []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var list []string
		_ = json.Unmarshal([]byte(raw), &list)
		images = append(images, list...)
	}
	rows.Close()
	result, err := s.db.Exec(`DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE chat_id = ?`, id); err != nil {
		return nil, err
	}
	return images, nil
}

func (s *Store) DeleteAllChats() ([]string, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	rows, err := s.db.Query(`SELECT images FROM messages`)
	if err != nil {
		return nil, err
	}
	var images []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var list []string
		_ = json.Unmarshal([]byte(raw), &list)
		images = append(images, list...)
	}
	rows.Close()
	if _, err := s.db.Exec(`DELETE FROM chats`); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		return nil, err
	}
	return images, nil
}

type legacyChatStore struct {
	Chats []struct {
		ID        string               `yaml:"id"`
		Title     string               `yaml:"title"`
		Model     string               `yaml:"model"`
		Messages  []config.ChatMessage `yaml:"messages"`
		CreatedAt string               `yaml:"created_at"`
		UpdatedAt string               `yaml:"updated_at"`
	} `yaml:"chats"`
}

func (s *Store) migrateLegacyChats() error {
	path := filepath.Join(s.dir, "chats.yaml")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		var legacy legacyChatStore
		if err := yaml.Unmarshal(raw, &legacy); err != nil {
			return fmt.Errorf("parse legacy chats: %w", err)
		}
		for _, old := range legacy.Chats {
			if old.ID == "" {
				continue
			}
			now := config.Now()
			chat := config.Chat{ID: old.ID, Title: old.Title, Model: old.Model, CreatedAt: old.CreatedAt, UpdatedAt: old.UpdatedAt}
			if chat.CreatedAt == "" {
				chat.CreatedAt = now
			}
			if chat.UpdatedAt == "" {
				chat.UpdatedAt = chat.CreatedAt
			}
			if err := s.CreateChat(chat); err != nil {
				return err
			}
			parentID := ""
			for _, message := range old.Messages {
				message.ParentID = parentID
				if message.CreatedAt == "" {
					message.CreatedAt = now
				}
				if err := s.InsertMessage(chat.ID, message); err != nil {
					return err
				}
				parentID = message.ID
			}
			if parentID != "" {
				if err := s.SetActiveMessage(chat.ID, parentID, ""); err != nil {
					return err
				}
			}
		}
	}
	_ = os.Rename(path, path+".migrated")
	return nil
}

type RedactionRecord struct {
	ID          int64  `json:"id"`
	RuleID      string `json:"rule_id"`
	RuleName    string `json:"rule_name"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
	CreatedAt   string `json:"created_at"`
}

func (s *Store) RecordRedactions(hits []redact.Hit) {
	if len(hits) == 0 {
		return
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	now := config.Now()
	for _, hit := range hits {
		if _, err := tx.Exec(`INSERT INTO redactions (rule_id, rule_name, original, replacement, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			hit.RuleID, hit.RuleName, hit.Original, hit.Replacement, now); err != nil {
			_ = tx.Rollback()
			return
		}
	}
	_, _ = tx.Exec(`DELETE FROM redactions WHERE created_at < ?`, redactionCutoff())
	_ = tx.Commit()
}

func (s *Store) RedactionCounts() map[string]int64 {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	rows, err := s.db.Query(`SELECT rule_id, count(*) FROM redactions WHERE created_at >= ? GROUP BY rule_id`, redactionCutoff())
	if err != nil {
		return map[string]int64{}
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err == nil {
			out[id] = count
		}
	}
	return out
}

func (s *Store) RedactionRecords(ruleID string, limit int) []RedactionRecord {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, rule_id, rule_name, original, replacement, created_at
		FROM redactions WHERE created_at >= ?`
	args := []any{redactionCutoff()}
	if ruleID != "" {
		query += ` AND rule_id = ?`
		args = append(args, ruleID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RedactionRecord
	for rows.Next() {
		var item RedactionRecord
		if err := rows.Scan(&item.ID, &item.RuleID, &item.RuleName, &item.Original, &item.Replacement, &item.CreatedAt); err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (s *Store) ClearRedactions() error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	_, err := s.db.Exec(`DELETE FROM redactions`)
	return err
}

func (s *Store) pruneRedactions() error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	_, err := s.db.Exec(`DELETE FROM redactions WHERE created_at < ?`, redactionCutoff())
	return err
}

func redactionCutoff() string {
	return time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
}
