package openai

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	gogpt "github.com/sashabaranov/go-gpt3"
	"go.uber.org/ratelimit"
	"strconv"
	"strings"
	"time"
)

const (
	HumanPrefix = "Human: "
	BotPrefix   = "Assistant: "
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
	var promptBuilder strings.Builder
	promptBuilder.WriteString(o.initialPrompt)
	promptBuilder.WriteString(GetCurrentDate())
	promptBuilder.WriteString("\n\n")

	for i := 0; i < len(messages); i++ {
		message := messages[i]
		if message.FromHuman {
			promptBuilder.WriteString(HumanPrefix)
		} else {
			promptBuilder.WriteString(BotPrefix)
			promptBuilder.WriteString(" ")
		}
		promptBuilder.WriteString(message.Text)
		if i != len(messages)-1 {
			promptBuilder.WriteString("\n\n")
		}
	}
	promptBuilder.WriteString(" <|endoftext|> ")

	// use Complete to get the bot's response
	completion, err := o.Complete(promptBuilder.String(), ctx, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		resultErr = multierror.Append(resultErr, err)
		return "", resultErr
	}
	zlog.Debug().Str("prompt", promptBuilder.String()).Msgf("completion: %s", completion)

	lines := strings.Split(completion, "\n")
	botLines := make([]string, 0)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.HasPrefix(line, BotPrefix) {
			line = strings.TrimPrefix(line, BotPrefix)
			botLines = append(botLines, line)
			break
		}
		botLines = append(botLines, line)
	}

	// join botLines in reverse order
	var botResponseBuilder strings.Builder
	for i := len(botLines) - 1; i >= 0; i-- {
		botResponseBuilder.WriteString(botLines[i])
		if i != 0 {
			botResponseBuilder.WriteString("\n")
		}
	}

	return botResponseBuilder.String(), nil
}

func (o *OpenAI) Complete(prompt string, ctx context.Context, zlog *zerolog.Logger) (string, error) {
	o.limiter.Take()
	var resultErr error
	completion, err := o.client.CreateCompletion(ctx, gogpt.CompletionRequest{
		Model:       gogpt.GPT3TextDavinci003,
		MaxTokens:   512,
		Prompt:      prompt,
		Temperature: 1.0,
		TopP:        0.9,
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
	o.client.HTTPClient.CloseIdleConnections()
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
