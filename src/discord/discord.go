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

package discord

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"sort"
	"src/aws"
	"src/openai"
	"strings"
	"sync"
)

type GuildID string
type ChannelID string
type ThreadID string

// IDsMap stores which guildIDs, channelIDs, and threadIDs the bot is listening to. It also uses a RWMutex to protect
// concurrent access.
type IDsMap struct {
	guildIDs     map[GuildID]bool
	channelIDs   map[ChannelID]bool
	threadIDs    map[ThreadID]bool
	sync.RWMutex // protects guildIDs, channelIDs, and threadIDs
}

func NewIDsMap(guildIDs []GuildID) IDsMap {
	guildIDsMap := make(map[GuildID]bool)
	for _, guildID := range guildIDs {
		guildIDsMap[guildID] = true
	}

	return IDsMap{
		guildIDs:   guildIDsMap,
		channelIDs: make(map[ChannelID]bool),
		threadIDs:  make(map[ThreadID]bool),
	}
}

type Config struct {
	RemoveCommands bool
	ChannelPrefix  string
}

type Discord struct {
	discordClient      *discordgo.Session
	openaiClient       *openai.OpenAI
	lockClient         aws.LockClient
	registeredCommands []*discordgo.ApplicationCommand
	config             Config
	idsMap             IDsMap
	zlog               *zerolog.Logger
}

type Command struct {
	Name        string
	Description string
	Type        discordgo.ApplicationCommandType
	Handler     func(s *discordgo.Session, i *discordgo.InteractionCreate)
	Options     []*discordgo.ApplicationCommandOption
}

func (d *Discord) getDiscordCommands() []Command {
	return []Command{
		{
			Name:        "ping",
			Description: "Ping the bot",
			Type:        discordgo.ChatApplicationCommand,
			Handler:     d.pingInteractionHandler,
			Options:     nil,
		},
		{
			Name:        "complete",
			Description: "Complete a prompt",
			Type:        discordgo.ChatApplicationCommand,
			Handler:     d.completeInteractionHandler,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "prompt",
					Description:  "The prompt to complete",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "image",
			Description: "Create an image from a prompt",
			Type:        discordgo.ChatApplicationCommand,
			Handler:     d.createImageInteractionHandler,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "prompt",
					Description:  "The prompt to use to create the image",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}
}

func (d *Discord) setupDiscordCommands(guildID string, zlog *zerolog.Logger) error {
	discordCommands := d.getDiscordCommands()

	commandHandlers := make(map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate))
	for _, discordCommand := range discordCommands {
		commandHandlers[discordCommand.Name] = discordCommand.Handler
	}

	// Handle channel creation or deletion
	d.discordClient.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelCreate) {
		err := d.updateChannels()
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to update channels")
		}
	})

	d.discordClient.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		err := d.updateChannels()
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to update channels")
		}
	})

	d.discordClient.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		d.idsMap.RLock()
		if _, ok := d.idsMap.channelIDs[ChannelID(i.ChannelID)]; !ok {
			return
		}
		d.idsMap.RUnlock()

		if i.Type == discordgo.InteractionApplicationCommand {
			if handler, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {

				// TODO track prompts in S3 for resumption
				getPayloadFromIteraction(i)
				lock, err := d.lockClient.Acquire(context.Background(), i.ID, "" /*data*/)

				if err != nil {
					zlog.Error().Err(err).Msg("Failed to acquire lock")
					return
				}
				defer func() {
					if err := d.lockClient.Release(context.Background(), lock.ID); err != nil {
						zlog.Error().Err(err).Msg("Failed to release lock")
					}
				}()

				if err := d.deferInteractionReply(s, i); err != nil {
					return
				}
				handler(s, i)
			}
		}
	})

	d.registeredCommands = make([]*discordgo.ApplicationCommand, 0)
	for _, discordCommand := range discordCommands {
		applicationCommand := discordgo.ApplicationCommand{
			Name:        discordCommand.Name,
			Description: discordCommand.Description,
			Type:        discordCommand.Type,
			Options:     discordCommand.Options,
		}
		zlog.Info().Interface("command", applicationCommand.Name).Msg("Registering command")
		command, err := d.discordClient.ApplicationCommandCreate(d.discordClient.State.User.ID, guildID, &applicationCommand)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to create Discord command")
			return err
		}
		d.registeredCommands = append(d.registeredCommands, command)
	}

	return nil
}

func (d *Discord) DebugApplicationCommands() {
	commands, err := d.discordClient.ApplicationCommands(d.discordClient.State.User.ID, "")
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to get application commands")
		return
	}

	for _, command := range commands {
		d.zlog.Info().Interface("command", command).Msg("Application command")
	}
}

func (d *Discord) updateChannels() error {
	d.idsMap.Lock()
	defer d.idsMap.Unlock()

	newChannelIDs := make(map[ChannelID]bool)
	for guildID := range d.idsMap.guildIDs {
		channels, err := d.discordClient.GuildChannels(string(guildID))
		if err != nil {
			d.zlog.Error().Err(err).Msg("Failed to get channels")
			return err
		}

		// Find channels prefixed with the channel prefix
		for _, channel := range channels {
			if strings.HasPrefix(channel.Name, d.config.ChannelPrefix) {
				d.zlog.Info().Str("channel", channel.Name).Str("id", channel.ID).Msg("Found channel")
				newChannelIDs[ChannelID(channel.ID)] = true
			}
		}
	}

	d.idsMap.channelIDs = newChannelIDs
	d.zlog.Info().Interface("channelIDs", newChannelIDs).Msg("Updated channel IDs")

	return nil
}

func NewDiscord(
	discordToken string,
	openaiClient *openai.OpenAI,
	lockClient aws.LockClient,
	guildID string,
	zlog *zerolog.Logger,
) (*Discord, error) {
	discordClient, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to create Discord client")
		return nil, err
	}

	discord := Discord{
		discordClient: discordClient,
		openaiClient:  openaiClient,
		lockClient:    lockClient,
		config: Config{
			RemoveCommands: false,
			ChannelPrefix:  "openai",
		},
		idsMap: NewIDsMap([]GuildID{GuildID(guildID)}),
		zlog:   zlog,
	}

	// Set intent to read message content
	discordClient.Identify.Intents |= discordgo.IntentsMessageContent

	err = discordClient.Open()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to open Discord client")
		return nil, err
	}

	err = discord.updateChannels()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to update channels")
		return nil, err
	}

	err = discord.updateThreads(zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to update threads")
		return nil, err
	}

	err = discord.setupDiscordCommands(guildID, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to setup Discord commands")
		return nil, err
	}

	discordClient.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		_, err := lockClient.Acquire(context.Background(), m.Message.ID, "")
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to acquire lock")
			return
		}
		defer func() {
			if err := lockClient.Release(context.Background(), m.Message.ID); err != nil {
				zlog.Error().Err(err).Msg("Failed to release lock")
			}
		}()

		zlog := zlog.With().Str("channel", m.ChannelID).Str("message", m.ID).Logger()

		// If the message is in a channel and it is not creating a thread, use it to create a thread.
		var maybeNewThread *discordgo.Channel = nil
		if shouldCreateThread := func() bool {
			discord.idsMap.RLock()
			defer discord.idsMap.RUnlock()

			if _, ok := discord.idsMap.channelIDs[ChannelID(m.ChannelID)]; !ok {
				return false
			}

			if m.Message.Flags&discordgo.MessageFlagsHasThread != 0 {
				return false
			}

			return true
		}(); shouldCreateThread {
			// Use OpenAI to summarize the message into a short title with less than 4 words.
			summary, err := discord.openaiClient.Summarize(m.Message.Content, 4, context.TODO(), &zlog)
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to summarize message")
				return
			}
			zlog.Info().Str("summary", summary).Msg("Summarized message")

			// See: https://github.com/bwmarrin/discordgo/blob/master/examples/threads/main.go
			maybeNewThread, err = s.MessageThreadStartComplex(m.ChannelID, m.ID, &discordgo.ThreadStart{
				Name:                summary,
				AutoArchiveDuration: 1440, /* 1 day */
				Invitable:           false,
				RateLimitPerUser:    1,
			})

			if err != nil {
				zlog.Error().Err(err).Msg("Failed to create thread")
				return
			}

			zlog.Debug().Str("thread", maybeNewThread.ID).Msg("Created thread")

			return
		}

		err = discord.updateThreads(&zlog)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to update thread IDs")
		}

		if unknownThread := func(threadID ThreadID) bool {
			discord.idsMap.RLock()
			defer discord.idsMap.RUnlock()

			if _, okThread := discord.idsMap.threadIDs[threadID]; !okThread {
				return true
			}
			return false

		}(ThreadID(m.ChannelID)); unknownThread {
			return
		}

		// Get all messages in the thread. Use a limit of 100 and use pagination of beforeID and afterID
		// to get all messages in the thread.
		messages := make([]*discordgo.Message, 0)
		beforeID := ""
		afterID := ""

		var channelID string
		if maybeNewThread != nil {
			channelID = maybeNewThread.ID
		} else {
			channelID = m.ChannelID
		}
		zlog.Debug().Str("channel", channelID).Msg("Getting messages")

		for {
			result, err := s.ChannelMessages(channelID, 100, beforeID, afterID, "")
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to get messages")
				return
			}

			// only append messages that have non-empty content
			for _, message := range result {
				if message.Content == "" {
					continue
				}
				messages = append(messages, message)
			}

			if len(result) < 100 {
				break
			}

			beforeID = result[len(result)-1].ID
		}

		// sort messages by id; since they are snowflakes this will be in chronological order
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].ID < messages[j].ID
		})

		// If a starter message exists, Discord re-uses the same ID for both this starter message and the thread itself.
		// Hence, listing messages in a thread cannot return the first message (!!!). You have to get the parent of the
		// thread, list messages in the thread, and find the message with the same ID at the thread (!!!).
		starterMessage, err := discord.FetchStarterMessage(m.ChannelID, &zlog)
		if err == nil {
			zlog.Info().
				Str("starter_message", starterMessage.ID).
				Str("author", starterMessage.Author.ID).
				Str("content", starterMessage.Content).
				Msg("Starter message")
			messages = append([]*discordgo.Message{starterMessage}, messages...)
		}

		for _, message := range messages {
			zlog.Info().Str("sub_message", message.ID).Str("author", message.Author.ID).Str("content", m.Content).Msg("Message")
		}

		lastMessage := messages[len(messages)-1]

		// If there is only one message, assume this is from a human.
		if len(messages) == 1 {
			messages[0].Author.Bot = false
		}

		// If the newest message in the thread is from a bot, we don't need to respond.
		if lastMessage.Author.Bot {
			zlog.Info().Msg("Newest message is from a bot, not responding")
			return
		}

		// Set a loading reaction on the newest message.
		err = s.MessageReactionAdd(m.ChannelID, lastMessage.ID, "🤖")
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to add reaction")
		}

		// convert messages to []*ChatMessage, call openaiClient.CompleteChat, and send the response to the thread
		chatMessages := make([]*openai.ChatMessage, 0)
		for _, message := range messages {
			fromHuman := !message.Author.Bot
			chatMessages = append(chatMessages, &openai.ChatMessage{
				FromHuman: fromHuman,
				Text:      message.Content,
			})
		}
		response, err := openaiClient.CompleteChat(chatMessages, context.TODO(), &zlog)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to complete chat")
			err = s.MessageReactionAdd(m.ChannelID, lastMessage.ID, "❌")
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to add reaction")
			}
			return
		}
		_, err = s.ChannelMessageSend(m.ChannelID, response)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to send message")
			err = s.MessageReactionAdd(m.ChannelID, lastMessage.ID, "❌")
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to add reaction")
			}
			return
		}

		err = s.MessageReactionAdd(m.ChannelID, lastMessage.ID, "✅")
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to add reaction")
		}
	})

	discordClient.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		zlog.Info().Interface("r", r).Msg("Discord client is now ready")
	})

	discord.DebugApplicationCommands()

	return &discord, nil
}

// see: https://github.com/discordjs/discord.js/blob/f3fe3ced622676b406a62b43f085aedde7a621aa/packages/discord.js/src/structures/ThreadChannel.js#L303-L315
func (d *Discord) FetchStarterMessage(threadID string, zlog *zerolog.Logger) (*discordgo.Message, error) {
	channel, err := d.discordClient.Channel(threadID)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to get thread")
		return nil, err
	}

	// Get the message whose ID is the same as the thread ID.
	message, err := d.discordClient.ChannelMessage(channel.ParentID, threadID)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to get parent message")
		return nil, err
	}
	return message, nil
}

func (d *Discord) updateThreads(zlog *zerolog.Logger) error {
	d.idsMap.Lock()
	defer d.idsMap.Unlock()

	newThreadIDs := make(map[ThreadID]bool)

	for channelID := range d.idsMap.channelIDs {
		result, err := d.discordClient.ThreadsActive(string(channelID))
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to get threads")
			return err
		}
		for _, thread := range result.Threads {
			newThreadIDs[ThreadID(thread.ID)] = true
		}
	}

	d.idsMap.threadIDs = newThreadIDs

	return nil
}

func (d *Discord) deferInteractionReply(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to defer interaction reply")
		return err
	}
	return nil
}

func (d *Discord) pingInteractionHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	payload := i.ApplicationCommandData()
	d.zlog.Info().Str("command", payload.Name).Interface("payload", payload).Msg("Received ping command")

	// Send the pong message by editing the original interaction response.
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: Ptr("Pong!"),
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to edit interaction response")
	}
}

func (d *Discord) completeInteractionHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	prompt := getPayloadFromIteraction(i)

	// Get the completion from OpenAI.
	ctx := context.Background()
	completion, err := d.openaiClient.Complete(prompt, ctx, d.zlog)
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to get completion from OpenAI")

		// Respond failure to the interaction with the contents of the error message.
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: Ptr(err.Error()),
		})

		return
	}
	completion = strings.TrimSpace(completion)

	// Create a response string, which is the original prompt in a quote block, followed by the completion.
	response := fmt.Sprintf("> %s\n\n%s", prompt, completion)

	// Respond to the interaction.
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: Ptr(response),
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to respond to interaction")
		return
	}
}

func (d *Discord) createImageInteractionHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	prompt := getPayloadFromIteraction(i)

	// Get the image URLs from OpenAI.
	ctx := context.Background()
	resp, err := d.openaiClient.CreateImage(prompt, ctx, d.zlog)
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to get completion from OpenAI")

		// Respond failure to the interaction with the contents of the error message.
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: Ptr(err.Error()),
		})

		return
	}

	response := fmt.Sprintf("> %s", prompt)
	files := make([]*discordgo.File, 0)
	for i := 0; i < len(resp.Images); i++ {
		name := fmt.Sprintf("image%d.png", i)
		files = append(files, &discordgo.File{
			Name:   name,
			Reader: bytes.NewReader(resp.Images[i].Data),
		})
	}

	// Respond to the interaction.
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: Ptr(response),
		Files:   files,
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to respond to interaction")
		return
	}
}

func (d *Discord) Close(zlog *zerolog.Logger) error {
	var resultError error

	if d.config.RemoveCommands {
		for _, command := range d.registeredCommands {
			zlog.Info().Interface("command", command).Msg("Deleting command")
			err := d.discordClient.ApplicationCommandDelete(d.discordClient.State.User.ID, "", command.ID)
			if err != nil {
				zlog.Error().Err(err).Msg("Failed to delete command")
				resultError = multierror.Append(resultError, err)
			}
		}
	}

	err := d.discordClient.Close()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to close Discord client")
		resultError = multierror.Append(resultError, err)
	}

	return resultError
}

func getPayloadFromIteraction(i *discordgo.InteractionCreate) string {
	payload := i.ApplicationCommandData()
	if len(payload.Options) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.Options[0].StringValue())
}

func Ptr[T any](t T) *T {
	return &t
}
