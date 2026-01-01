package chat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/guanke/papaya/internal/store"
)

const (
	maxHistory             = 10
	defaultRateLimitPerMin = 20
)

var defaultSystemMessages = []openai.ChatCompletionMessage{
	{
		Role:    openai.ChatMessageRoleSystem,
		Content: "你是一名友好、简洁且乐于助人的助手，请在回答时尽量给出明确、可执行的建议。必要时可以用有序列表展示步骤。",
	},
	{
		Role:    openai.ChatMessageRoleSystem,
		Content: "如果用户的问题不完整，先提出澄清问题；如果问题已解决，可以简短总结。",
	},
}

// Manager coordinates chat completion calls and per-user context.
type Manager struct {
	client    *openai.Client
	store     *store.Store
	model     string
	histories map[int64][]openai.ChatCompletionMessage
	rateLimit int
	rates     map[int64]rateWindow
	mu        sync.Mutex
}

type rateWindow struct {
	start time.Time
	count int
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

	rateLimit := defaultRateLimitPerMin
	if st != nil {
		if stored, ok, err := st.GetRateLimit(); err == nil && ok {
			rateLimit = stored
		}
	}

	return &Manager{
		client:    c,
		store:     st,
		model:     model,
		histories: make(map[int64][]openai.ChatCompletionMessage),
		rateLimit: rateLimit,
		rates:     make(map[int64]rateWindow),
	}
}

// Chat sends a prompt to the OpenAI-style API and manages conversation context.
func (m *Manager) Chat(ctx context.Context, userID int64, prompt string) (string, error) {
	m.mu.Lock()
	if m.client == nil {
		m.mu.Unlock()
		return "", errors.New("OpenAI client is not configured")
	}
	if err := m.consumeRate(userID); err != nil {
		m.mu.Unlock()
		return "", err
	}
	messages := append([]openai.ChatCompletionMessage{}, defaultSystemMessages...)
	messages = append(messages, m.histories[userID]...)
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

// SetRateLimit updates the allowed chats per minute. A value <=0 disables the limit.
func (m *Manager) SetRateLimit(limit int) error {
	m.mu.Lock()
	m.rateLimit = limit
	m.rates = make(map[int64]rateWindow) // reset windows when changing limit
	m.mu.Unlock()
	if m.store == nil {
		return nil
	}
	return m.store.SetRateLimit(limit)
}

// RateLimit returns current per-minute limit (<=0 means disabled).
func (m *Manager) RateLimit() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rateLimit
}

// ListModels fetches available model IDs from the API.
func (m *Manager) ListModels(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil, errors.New("OpenAI client is not configured")
	}
	resp, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(resp.Models))
	for _, model := range resp.Models {
		models = append(models, model.ID)
	}
	sort.Strings(models)
	return models, nil
}

// consumeRate assumes m.mu is held; it increments the current window and returns an error if exceeded.
func (m *Manager) consumeRate(userID int64) error {
	limit := m.rateLimit
	if limit <= 0 {
		return nil
	}
	now := time.Now()
	window := m.rates[userID]
	if window.start.IsZero() || now.Sub(window.start) >= time.Minute {
		window = rateWindow{start: now, count: 0}
	}
	if window.count >= limit {
		return fmt.Errorf("已达到每分钟 %d 次聊天上限，请稍后再试。", limit)
	}
	window.count++
	m.rates[userID] = window
	return nil
}
