package hub

import (
	"errors"
	"strings"
	"sync"
)

var (
	ErrUserRegDisabled = errors.New("user registration is disabled")
)

type UserDatabase interface {
	IsRegistered(name string) (bool, error)
	GetUserPassword(name string) (string, error)
	RegisterUser(name, pass string) error
}

func (h *Hub) validateUserName(name string) error {
	if name == "" {
		return errors.New("name should be empty")
	} else if strings.HasPrefix(name, "#") {
		return errors.New("name should not start with '#'")
	} else if strings.HasPrefix(name, "!") {
		return errors.New("name should not start with '!'")
	} else if name != strings.TrimSpace(name) {
		return errors.New("name should not start or end with spaces")
	}
	return nil
}

func (h *Hub) RegisterUser(name, pass string) error {
	if h.userDB == nil {
		return ErrUserRegDisabled
	}
	return h.userDB.RegisterUser(name, pass)
}

func (h *Hub) IsRegistered(name string) (bool, error) {
	if h.userDB == nil {
		return false, nil
	}
	return h.userDB.IsRegistered(name)
}

// NewUserDatabase creates an in-memory users database.
func NewUserDatabase() UserDatabase {
	return &memUsersDB{
		users: make(map[string]string),
	}
}

type memUsersDB struct {
	mu    sync.RWMutex
	users map[string]string
}

func (db *memUsersDB) IsRegistered(name string) (bool, error) {
	db.mu.RLock()
	_, ok := db.users[name]
	db.mu.RUnlock()
	return ok, nil
}

func (db *memUsersDB) GetUserPassword(name string) (string, error) {
	db.mu.RLock()
	pass := db.users[name]
	db.mu.RUnlock()
	return pass, nil
}

func (db *memUsersDB) RegisterUser(name, pass string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.users[name] = pass
	return nil
}