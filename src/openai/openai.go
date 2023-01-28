package openai

import (
	"context"
	"errors"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	gogpt "github.com/sashabaranov/go-gpt3"
)

var (
	FailedToCompletePrompt = errors.New("failed to complete prompt")
)

type OpenAI struct {
	client *gogpt.Client
}

func NewOpenAI(token string) *OpenAI {
	client := gogpt.NewClient(token)
	return &OpenAI{
		client,
	}
}

func (o *OpenAI) Complete(prompt string, ctx context.Context, zlog *zerolog.Logger) (string, error) {
	var resultErr error
	completion, err := o.client.CreateCompletion(ctx, gogpt.CompletionRequest{
		Model:     gogpt.GPT3TextDavinci003,
		MaxTokens: 300,
		Prompt:    prompt,
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		resultErr = multierror.Append(resultErr, err, FailedToCompletePrompt)
		return "", resultErr
	}
	return completion.Choices[0].Text, resultErr
}

func (o *OpenAI) Close(*zerolog.Logger) error {
	o.client.HTTPClient.CloseIdleConnections()
	return nil
}
