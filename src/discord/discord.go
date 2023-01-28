package discord

import (
	"context"
	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"src/openai"
)

const (
	tokenEnvName = "DISCORD_TOKEN"
)

type DiscordConfig struct {
	RemoveCommands bool
}

type Discord struct {
	discordClient      *discordgo.Session
	openaiClient       *openai.OpenAI
	registeredCommands []*discordgo.ApplicationCommand
	config             DiscordConfig
	zlog               *zerolog.Logger
}

type DiscordCommand struct {
	Name        string
	Description string
	Type        discordgo.ApplicationCommandType
	Handler     func(s *discordgo.Session, i *discordgo.InteractionCreate)
	Options     []*discordgo.ApplicationCommandOption
}

func (d *Discord) getDiscordCommands() []DiscordCommand {
	return []DiscordCommand{
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
			Handler:     d.completeInterationHandler,
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

func NewDiscord(discordToken string, openaiClient *openai.OpenAI, zlog *zerolog.Logger) (*Discord, error) {
	discordClient, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to create Discord client")
		return nil, err
	}

	discord := Discord{
		discordClient: discordClient,
		openaiClient:  openaiClient,
		config: DiscordConfig{
			RemoveCommands: false,
		},
		zlog: zlog,
	}

	err = discordClient.Open()
	if err != nil {
		zlog.Error().Err(err).Msg("Failed to open Discord client")
		return nil, err
	}

	guildID := ""
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

func (d *Discord) pingInteractionHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	payload := i.ApplicationCommandData()
	d.zlog.Info().Str("command", payload.Name).Interface("payload", payload).Msg("Received ping command")
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Pong!",
		},
	})
}

func (d *Discord) completeInterationHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Send an initial response to acknowledge the command.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to send initial response to interaction")
		return
	}

	// Get the payload from the interaction.
	payload := i.ApplicationCommandData()
	d.zlog.Info().Str("command", payload.Name).Interface("payload", payload).Msg("Received command")

	// Get the prompt from the payload.
	prompt := payload.Options[0].StringValue()

	// Get the completion from OpenAI.
	ctx := context.Background()
	completion, err := d.openaiClient.Complete(prompt, ctx, d.zlog)
	if err != nil {
		d.zlog.Error().Err(err).Msg("Failed to get completion from OpenAI")
		return
	}

	// Respond to the interaction.
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: completion,
		},
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
