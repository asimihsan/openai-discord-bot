/*
 * Copyright (C) 2023 Asim Ihsan
 * SPDX-License-Identifier: AGPL-3.0-only
 *
 * This program is free software: you can redistribute it and/or modify it under
 * the terms of the GNU Affero General Public License as published by the Free
 * Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License along
 * with this program. If not, see <https://www.gnu.org/licenses/>
 */

package openai

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	goopenai "github.com/sashabaranov/go-openai"
	"go.uber.org/ratelimit"
	"io"
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
	client        *goopenai.Client
	initialPrompt string
	limiter       ratelimit.Limiter
}

func NewOpenAI(token string) *OpenAI {
	client := goopenai.NewClient(token)
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

func ConvertChatMessagesToChatCompletionMessages(messages []*ChatMessage) []goopenai.ChatCompletionMessage {
	requestMessages := make([]goopenai.ChatCompletionMessage, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		message := messages[i]
		if message.FromHuman {
			requestMessages = append(requestMessages, goopenai.ChatCompletionMessage{
				Role:    "user",
				Content: message.Text,
			})
		} else {
			requestMessages = append(requestMessages, goopenai.ChatCompletionMessage{
				Role:    "assistant",
				Content: message.Text,
			})
		}
	}

	return requestMessages
}

func (o *OpenAI) CompleteChat(messages []*ChatMessage, ctx context.Context, zlog *zerolog.Logger) (string, error) {
	o.limiter.Take()
	var resultErr error
	requestMessages := ConvertChatMessagesToChatCompletionMessages(messages)

	completion, err := o.ChatComplete(requestMessages, ctx, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete prompt")
		resultErr = multierror.Append(resultErr, err)
		return "", resultErr
	}
	zlog.Debug().Interface("requestMessages", requestMessages).Msgf("completion: %s", completion)

	return completion, nil
}

func (o *OpenAI) ChatCompleteStream(
	messages []*ChatMessage,
	outputChannel chan string,
	errChannel chan error,
	cancelChannel chan bool,
	ctx context.Context,
	zlog *zerolog.Logger,
) error {
	requestMessages := ConvertChatMessagesToChatCompletionMessages(messages)

	o.limiter.Take()
	var resultErr error
	stream, err := o.client.CreateChatCompletionStream(ctx, goopenai.ChatCompletionRequest{
		Model:       goopenai.GPT4,
		Messages:    requestMessages,
		MaxTokens:   4096,
		Temperature: 0.0,
		TopP:        1.0,
		Stream:      false,
		Stop:        []string{"<|endoftext|>"},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to complete chat")
		resultErr = multierror.Append(resultErr, err, FailedToCompletePrompt)
		return resultErr
	}
	defer stream.Close()

	for {
		select {
		case <-cancelChannel:
			zlog.Debug().Msg("Canceling chat completion stream")
			close(outputChannel)
			return nil
		default:
			completion, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				zlog.Debug().Msg("Chat completion stream EOF")
				close(outputChannel)
				close(errChannel)
				return nil
			}
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to complete chat")
				resultErr = multierror.Append(resultErr, err, FailedToCompletePrompt)
				errChannel <- err
				close(outputChannel)
				return resultErr
			}
			content := completion.Choices[0].Delta.Content
			zlog.Debug().Msgf("completion delta: %s", content)
			outputChannel <- content
		}
	}
}

func (o *OpenAI) ChatComplete(
	messages []goopenai.ChatCompletionMessage,
	ctx context.Context,
	zlog *zerolog.Logger,
) (string, error) {
	o.limiter.Take()
	var resultErr error
	completion, err := o.client.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model:       goopenai.GPT4,
		Messages:    messages,
		MaxTokens:   4096,
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
	completion, err := o.client.CreateCompletion(ctx, goopenai.CompletionRequest{
		Model:       goopenai.GPT3TextDavinci003,
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
	resp, err := o.client.CreateImage(ctx, goopenai.ImageRequest{
		Prompt:         prompt,
		N:              1,
		Size:           goopenai.CreateImageSize1024x1024,
		ResponseFormat: goopenai.CreateImageResponseFormatB64JSON,
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

	completion, err := o.client.CreateCompletion(ctx, goopenai.CompletionRequest{
		Model:     goopenai.GPT4,
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
