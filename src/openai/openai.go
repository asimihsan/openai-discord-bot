package openai

import (
	"context"
	"encoding/base64"
	"errors"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	gogpt "github.com/sashabaranov/go-gpt3"
	"go.uber.org/ratelimit"
)

var (
	FailedToCompletePrompt = errors.New("failed to complete prompt")
)

type OpenAI struct {
	client  *gogpt.Client
	limiter ratelimit.Limiter
}

func NewOpenAI(token string) *OpenAI {
	client := gogpt.NewClient(token)
	limiter := ratelimit.New(1)
	return &OpenAI{
		client,
		limiter,
	}
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
