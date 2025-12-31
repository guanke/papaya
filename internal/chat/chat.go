package chat

import (
	"context"
	"errors"
	"sync"

	"github.com/sashabaranov/go-openai"

	"github.com/guanke/papaya/internal/store"
)

const maxHistory = 10

// Manager coordinates chat completion calls and per-user context.
type Manager struct {
	client    *openai.Client
	store     *store.Store
	model     string
	histories map[int64][]openai.ChatCompletionMessage
	mu        sync.Mutex
}

// NewManager creates a new chat manager. If apiKey is empty, the Manager is created
// without a client and will return errors when Chat is invoked.
func NewManager(apiKey, baseURL, model string, st *store.Store) *Manager {
	var c *openai.Client
	if apiKey != "" {
		config := openai.DefaultConfig(apiKey)
		if baseURL != "" {
			config.BaseURL = baseURL
		}
		client := openai.NewClientWithConfig(config)
		c = client
	}

	return &Manager{
		client:    c,
		store:     st,
		model:     model,
		histories: make(map[int64][]openai.ChatCompletionMessage),
	}
}

// Chat sends a prompt to the OpenAI-style API and manages conversation context.
func (m *Manager) Chat(ctx context.Context, userID int64, prompt string) (string, error) {
	m.mu.Lock()
	if m.client == nil {
		m.mu.Unlock()
		return "", errors.New("OpenAI client is not configured")
	}
	messages := append([]openai.ChatCompletionMessage{}, m.histories[userID]...)
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: prompt})
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	model := m.model
	if stored, err := m.store.GetModel(); err == nil && stored != "" {
		model = stored
	}
	m.mu.Unlock()

	resp, err := m.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty response")
	}
	answer := resp.Choices[0].Message.Content

	m.mu.Lock()
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: answer})
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	m.histories[userID] = messages
	m.mu.Unlock()

	return answer, nil
}

// SetModel updates the in-memory default model and persists it in the store.
func (m *Manager) SetModel(model string) error {
	m.mu.Lock()
	m.model = model
	m.mu.Unlock()
	return m.store.SetModel(model)
}

// Model returns the current model preference.
func (m *Manager) Model() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.model
}
