package chat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"encoding/json"
	
	"github.com/sashabaranov/go-openai"

	"github.com/guanke/papaya/internal/store"
)


const (
	maxHistory             = 20
	summaryThreshold       = 15
	keepRecent             = 5
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
	token     string
	baseURL   string
	model     string
	rateLimit float64 // tokens per minute or requests per minute? Let's say requests per minute for simplicity
	rates     map[string]rateWindow
	mu        sync.Mutex
	histories map[string][]openai.ChatCompletionMessage // userID (string) -> history
	store     *store.Store
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

	rateLimit := float64(defaultRateLimitPerMin)
	if st != nil {
		if stored, ok, err := st.GetRateLimit(); err == nil && ok {
			rateLimit = float64(stored)
		}
	}

	return &Manager{
		client:    c,
		token:     apiKey, // Assuming apiKey is the token
		baseURL:   baseURL,
		model:     model,
		rateLimit: rateLimit,
		histories: make(map[string][]openai.ChatCompletionMessage),
		rates:     make(map[string]rateWindow),
		store:     st,
	}
}

// Chat sends a prompt to the OpenAI-style API and manages conversation context.
func (m *Manager) Chat(ctx context.Context, user *store.User, prompt string) (string, error) {
	m.mu.Lock()
	// Check rate limit (simple implementation)
	// For production, use a token bucket per user.
	// Here we just limit globally for simplicity or maybe we don't need it yet.
	// Let's implement per-user check in Store or memory.
	
	userID := user.ID
	if m.client == nil {
		m.mu.Unlock()
		return "", errors.New("OpenAI client is not configured")
	}
	if err := m.consumeRate(userID); err != nil {
		m.mu.Unlock()
		return "", err
	}
	
    // Construct system messages
    var systemMessages []openai.ChatCompletionMessage
    if user.Persona != "" {
        systemMessages = append(systemMessages, openai.ChatCompletionMessage{
            Role: openai.ChatMessageRoleSystem,
            Content: user.Persona,
        })
    }
    systemMessages = append(systemMessages, defaultSystemMessages...)

    // Load history from store if not in memory
    var history []openai.ChatCompletionMessage
    if h, ok := m.histories[userID]; ok {
        history = h
    } else {
        // Try load from store
        if m.store != nil {
            data, err := m.store.GetChatHistory(userID)
             if err == nil && len(data) > 0 {
                if err := json.Unmarshal(data, &history); err != nil {
                    // Log error? For now just ignore corrupt history
                } else {
                     m.histories[userID] = history
                }
             }
        }
    }
    
    // Auto-Summarization Logic
    if len(history) > summaryThreshold {
        // Split: older vs recent
        // We want to keep `keepRecent` messages intact at the end.
        cutIdx := len(history) - keepRecent
        if cutIdx > 0 {
            older := history[:cutIdx]
            recent := history[cutIdx:]
            
            // Generate summary for 'older'
            summary, err := m.summarizeHistory(ctx, older)
            if err == nil {
                // Construct new history: [System(Summary)] + [Recent]
                // We mark the summary as System role to give it context weight, 
                // but distinguished from the main system prompt.
                newHistory := []openai.ChatCompletionMessage{
                    {
                        Role: openai.ChatMessageRoleSystem,
                        Content: fmt.Sprintf("Prior conversation summary: %s", summary),
                    },
                }
                newHistory = append(newHistory, recent...)
                history = newHistory
                
                // Update memory and persist immediately
                m.histories[userID] = history
                if m.store != nil {
                    if data, err := json.Marshal(history); err == nil {
                        go m.store.SaveChatHistory(userID, data)
                    }
                }
            } else {
                // If summary fails, just log/ignore? 
                // We can proceed without summary but we might hit token limits eventually.
                // For now, let's just proceed.
            }
        }
    }

	messages := append([]openai.ChatCompletionMessage{}, systemMessages...)
	messages = append(messages, history...)
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: prompt})
	if len(messages) > maxHistory {
		// Note: we usually filter system messages out before counting? 
		// Actually defaultSystemMessages are appended every time. 
		// History contains only User/Assistant exchanges.
		// Wait, `messages` line 90 (original) appends history. 
		// Let's refine.
		// `m.histories` should store ONLY conversation turns.
		// `messages` here is the *request* payload which includes system prompt.
		
		// Logic check:
		// 1. system messages (dynamic)
		// 2. history (context)
		// 3. current user prompt
		// 4. (later) append current user prompt + assistant answer to history
	}
	// To respect context limit for the request, we trim IF total is too huge. 
	// But `maxHistory` usually constrains the `history` slice itself.
	
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
	// Reload history in case of parallel requests (though simple lock handles it)
	// We need to append request prompt + answer to history.
	// `messages` above included system prompts, which we DO NOT want in history.
	// We want to append: User Prompt, Assistant Answer.
	
	currentHistory := m.histories[userID]
	currentHistory = append(currentHistory, 
	    openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: prompt},
	    openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: answer},
	)
	
	if len(currentHistory) > maxHistory {
		currentHistory = currentHistory[len(currentHistory)-maxHistory:]
	}
	m.histories[userID] = currentHistory
	
	// Persist
	if m.store != nil {
	    if data, err := json.Marshal(currentHistory); err == nil {
	        go m.store.SaveChatHistory(userID, data)
	    }
	}
	
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

// SetRateLimit sets the global rate limit (per minute).
func (m *Manager) SetRateLimit(limit int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rateLimit = float64(limit)
	if m.store != nil {
		return m.store.SetRateLimit(limit)
	}
	return nil
}

// RateLimit returns the current rate limit.
func (m *Manager) RateLimit() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int(m.rateLimit)
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
func (m *Manager) consumeRate(userID string) error {
	if m.rateLimit <= 0 {
		return nil
	}
	now := time.Now()
	// Clean up old windows? Not strictly necessary for simple bot, we can just reset if > 1 min.
	// We only track the current user.

	window, exists := m.rates[userID]
	if !exists || now.Sub(window.start) > time.Minute {
		// New window
		m.rates[userID] = rateWindow{start: now, count: 1}
		return nil
	}

	if float64(window.count) >= m.rateLimit {
		return fmt.Errorf("rate limit exceeded (max %d/min), please try again later", int(m.rateLimit))
	}

	window.count++
	m.rates[userID] = window
	return nil
}

// AnalyzeImage uses GPT-4 Vision to generate tags for an image URL.
func (m *Manager) AnalyzeImage(ctx context.Context, imageURL string) ([]string, error) {
    m.mu.Lock()
    if m.client == nil {
        m.mu.Unlock()
        return nil, errors.New("OpenAI client is not configured")
    }
    m.mu.Unlock()

    resp, err := m.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: openai.GPT4VisionPreview, // Or generic GPT-4-turbo
        Messages: []openai.ChatCompletionMessage{
            {
                Role: openai.ChatMessageRoleSystem,
                Content: "You are an image tagging bot. Return strictly a comma-separated list of 3-5 tags in English (lowercased) describing the image content. Do not use sentences.",
            },
            {
                Role: openai.ChatMessageRoleUser,
                MultiContent: []openai.ChatMessagePart{
                    {
                        Type: openai.ChatMessagePartTypeImageURL,
                        ImageURL: &openai.ChatMessageImageURL{
                            URL: imageURL,
                            Detail: openai.ImageURLDetailLow,
                        },
                    },
                },
            },
        },
        MaxTokens: 100,
    })
    if err != nil {
        return nil, err
    }
    if len(resp.Choices) == 0 {
        return nil, errors.New("empty response from vision")
    }
    
    // Parse tags
    content := resp.Choices[0].Message.Content
    // Simple split
    // e.g. "sunset, landscape, mountain"
    parts := strings.Split(content, ",")
    var tags []string
    for _, p := range parts {
        t := strings.TrimSpace(p)
        t = strings.ToLower(t)
        if t != "" {
            tags = append(tags, t)
        }
    }
    return tags, nil
}

// summarizeHistory compresses a list of messages into a single string.
func (m *Manager) summarizeHistory(ctx context.Context, msgs []openai.ChatCompletionMessage) (string, error) {
    // We create a separate strict context for summarization to avoid infinite recursion or polluting main context?
    // Using the same client is fine.
    
    // Convert msgs to a readable text block
    var sb strings.Builder
    for _, msg := range msgs {
        role := msg.Role
        if role == openai.ChatMessageRoleSystem {
            // If there's an existing summary (System), treat it as "Old Context"
            role = "Context"
        }
        sb.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
    }
    conversation := sb.String()
    
    req := openai.ChatCompletionRequest{
        Model: openai.GPT3Dot5Turbo, // Use a cheaper model for summarization if possible, or same model.
        // Let's use m.model or default to 3.5-turbo.
        Messages: []openai.ChatCompletionMessage{
            {
                Role: openai.ChatMessageRoleSystem,
                Content: "You are a helpful assistant. Summarize the following conversation history into a single concise paragraph. Retain key facts, user name, preferences, and the current topic. Do not lose important details.",
            },
            {
                Role: openai.ChatMessageRoleUser,
                Content: conversation,
            },
        },
    }
    
    // We can use a separate timeout for summary
    sumCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    resp, err := m.client.CreateChatCompletion(sumCtx, req)
    if err != nil {
        return "", err
    }
    if len(resp.Choices) == 0 {
        return "", errors.New("empty summary response")
    }
    return resp.Choices[0].Message.Content, nil
}
