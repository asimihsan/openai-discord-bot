package openai

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	gogpt "github.com/sashabaranov/go-gpt3"
	"go.uber.org/ratelimit"
	"strconv"
	"strings"
	"time"
)

var (
	FailedToCompletePrompt = errors.New("failed to complete prompt")

	//go:embed initial_prompt_01.txt
	initialPrompt string
)

type OpenAI struct {
	client        *gogpt.Client
	initialPrompt string
	limiter       ratelimit.Limiter
}

func NewOpenAI(token string) *OpenAI {
	client := gogpt.NewClient(token)
	limiter := ratelimit.New(1)

	return &OpenAI{
		client:        client,
		initialPrompt: initialPrompt,
		limiter:       limiter,
	}
}

type ChatMessage struct {
	FromHuman bool
	Text      string
}

// GetCurrentDate returns the current date e.g. 2023-02-04.
func GetCurrentDate() string {
	now := time.Now().Unix()
	tm := time.Unix(now, 0)
	return tm.Format("2006-01-02")
}

func (o *OpenAI) CompleteChat(messages []*ChatMessage, ctx context.Context, zlog *zerolog.Logger) (string, error) {
	o.limiter.Take()
	var resultErr error
	requestMessages := make([]gogpt.ChatCompletionMessage, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		message := messages[i]
		if message.FromHuman {
			requestMessages = append(requestMessages, gogpt.ChatCompletionMessage{
				Role:    "user",
				Content: message.Text,
			})
		} else {
			requestMessages = append(requestMessages, gogpt.ChatCompletionMessage{
				Role:    "assistant",
				Content: message.Text,
			})
		}
	}

	completion, err := o.ChatComplete(requestMessages, ctx, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		resultErr = multierror.Append(resultErr, err)
		return "", resultErr
	}
	zlog.Debug().Interface("requestMessages", requestMessages).Msgf("completion: %s", completion)

	return completion, nil
}

func (o *OpenAI) ChatComplete(
	messages []gogpt.ChatCompletionMessage,
	ctx context.Context,
	zlog *zerolog.Logger,
) (string, error) {
	o.limiter.Take()
	var resultErr error
	completion, err := o.client.CreateChatCompletion(ctx, gogpt.ChatCompletionRequest{
		Model:       gogpt.GPT3Dot5Turbo,
		Messages:    messages,
		MaxTokens:   2048,
		Temperature: 0.0,
		TopP:        1.0,
		Stream:      false,
		Stop:        []string{"<|endoftext|>"},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete chat")
		resultErr = multierror.Append(resultErr, err, FailedToCompletePrompt)
		return "", resultErr
	}
	return completion.Choices[0].Message.Content, resultErr
}

func (o *OpenAI) Complete(prompt string, ctx context.Context, zlog *zerolog.Logger) (string, error) {
	o.limiter.Take()
	var resultErr error
	completion, err := o.client.CreateCompletion(ctx, gogpt.CompletionRequest{
		Model:       gogpt.GPT3TextDavinci003,
		MaxTokens:   2048,
		Prompt:      prompt,
		Temperature: 0.0,
		TopP:        1.0,
		Stop:        []string{"<|endoftext|>"},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		resultErr = multierror.Append(resultErr, err, FailedToCompletePrompt)
		return "", resultErr
	}
	return completion.Choices[0].Text, resultErr
}

type CreateImageResponse struct {
	Images []Image `json:"images"`
}

type Image struct {
	Data []byte `json:"data"`
}

func (o *OpenAI) CreateImage(prompt string, ctx context.Context, zlog *zerolog.Logger) (*CreateImageResponse, error) {
	o.limiter.Take()
	resp, err := o.client.CreateImage(ctx, gogpt.ImageRequest{
		Prompt:         prompt,
		N:              1,
		Size:           gogpt.CreateImageSize1024x1024,
		ResponseFormat: gogpt.CreateImageResponseFormatB64JSON,
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to create image")
		return nil, err
	}

	result := CreateImageResponse{Images: make([]Image, 0, len(resp.Data))}
	for _, data := range resp.Data {
		imageData, err := base64.StdEncoding.DecodeString(data.B64JSON)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to decode image data")
			return nil, err
		}
		result.Images = append(result.Images, Image{Data: imageData})
	}

	return &result, nil
}

func (o *OpenAI) Close(*zerolog.Logger) error {
	//o.client.HTTPClient.CloseIdleConnections()
	return nil
}

func (o *OpenAI) Summarize(
	content string,
	words int,
	ctx context.Context,
	zlog *zerolog.Logger,
) (string, error) {
	o.limiter.Take()

	var promptBuilder strings.Builder
	promptBuilder.WriteString(o.initialPrompt)
	promptBuilder.WriteString(GetCurrentDate())
	promptBuilder.WriteString("\n\n")
	promptBuilder.WriteString("Summarize the following message into less than ")
	promptBuilder.WriteString(strconv.Itoa(words))
	promptBuilder.WriteString(" words:\n\n")
	promptBuilder.WriteString(content)
	prompt := promptBuilder.String()

	completion, err := o.client.CreateCompletion(ctx, gogpt.CompletionRequest{
		Model:     gogpt.GPT3TextDavinci003,
		MaxTokens: 64,
		Prompt:    prompt,
		Stop:      []string{"<|endoftext|>"},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		return "", err
	}

	// trim space from summary
	summary := strings.TrimSpace(completion.Choices[0].Text)

	// trim punctuation from summary
	summary = strings.TrimRight(summary, ".")

	return summary, err
}
