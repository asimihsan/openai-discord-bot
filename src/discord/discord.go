package discord

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"src/aws"
	"src/openai"
	"strings"
)

type Config struct {
	RemoveCommands bool
}

type Discord struct {
	discordClient      *discordgo.Session
	openaiClient       *openai.OpenAI
	lockClient         aws.LockClient
	registeredCommands []*discordgo.ApplicationCommand
	config             Config
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

	d.discordClient.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type == discordgo.InteractionApplicationCommand {
			if handler, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
				prompt := getPayloadFromIteraction(i)
				lock, err := d.lockClient.Acquire(context.Background(), i.ID, prompt)
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
		},
		zlog: zlog,
	}

	err = discordClient.Open()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to open Discord client")
		return nil, err
	}

	err = discord.setupDiscordCommands(guildID, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to setup Discord commands")
		return nil, err
	}

	discordClient.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		zlog.Info().Interface("r", r).Msg("Discord client is now ready")
	})

	discord.DebugApplicationCommands()

	return &discord, nil
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

	err = d.lockClient.Close()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to close lock client")
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
